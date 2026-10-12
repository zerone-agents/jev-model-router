package provider

import (
	"encoding/json"
	"unicode/utf8"
)

type objectPrefixKind uint8

const (
	objectPrefixInvalid objectPrefixKind = iota
	objectPrefixIncomplete
	objectPrefixComplete
)
const (
	prefixObjectFirst = iota
	prefixObjectKey
	prefixColon
	prefixValue
	prefixObjectEnd
	prefixArrayFirst
	prefixArrayValue
	prefixArrayEnd
)

type prefixContainer struct {
	state int
	keys  map[string]bool
}

// classifyObjectPrefix recognizes the prefix language of a single JSON object.
// It never repairs input or expands numbers, and uses a bounded explicit stack.
func classifyObjectPrefix(raw []byte) objectPrefixKind {
	i := 0
	space := func() {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
			i++
		}
	}
	space()
	if i == len(raw) {
		return objectPrefixIncomplete
	}
	if raw[i] != '{' {
		return objectPrefixInvalid
	}
	i++
	stack := []prefixContainer{{state: prefixObjectFirst, keys: map[string]bool{}}}
	for len(stack) > 0 {
		space()
		if i == len(raw) {
			return objectPrefixIncomplete
		}
		f := &stack[len(stack)-1]
		switch f.state {
		case prefixObjectFirst, prefixObjectKey:
			if raw[i] == '}' && f.state == prefixObjectFirst {
				i++
				stack = stack[:len(stack)-1]
				continue
			}
			if raw[i] != '"' {
				return objectPrefixInvalid
			}
			start := i
			end, kind := prefixString(raw, i)
			if kind != objectPrefixComplete {
				return kind
			}
			i = end
			var key string
			_ = json.Unmarshal(raw[start:end], &key)
			if f.keys[key] {
				return objectPrefixInvalid
			}
			f.keys[key] = true
			f.state = prefixColon
			continue
		case prefixColon:
			if raw[i] != ':' {
				return objectPrefixInvalid
			}
			i++
			f.state = prefixValue
			continue
		case prefixObjectEnd:
			if raw[i] == '}' {
				i++
				stack = stack[:len(stack)-1]
				continue
			}
			if raw[i] != ',' {
				return objectPrefixInvalid
			}
			i++
			f.state = prefixObjectKey
			continue
		case prefixArrayEnd:
			if raw[i] == ']' {
				i++
				stack = stack[:len(stack)-1]
				continue
			}
			if raw[i] != ',' {
				return objectPrefixInvalid
			}
			i++
			f.state = prefixArrayValue
			continue
		case prefixArrayFirst:
			if raw[i] == ']' {
				i++
				stack = stack[:len(stack)-1]
				continue
			}
		}
		// The parent now expects its delimiter once this value finishes.
		if f.state == prefixValue {
			f.state = prefixObjectEnd
		} else {
			f.state = prefixArrayEnd
		}
		switch raw[i] {
		case '{', '[':
			if len(stack) >= 10000 {
				return objectPrefixInvalid
			}
			state := prefixArrayFirst
			var keys map[string]bool
			if raw[i] == '{' {
				state = prefixObjectFirst
				keys = map[string]bool{}
			}
			stack = append(stack, prefixContainer{state: state, keys: keys})
			i++
		case '"':
			end, kind := prefixString(raw, i)
			if kind != objectPrefixComplete {
				return kind
			}
			i = end
		case 't', 'f', 'n':
			literal := "true"
			if raw[i] == 'f' {
				literal = "false"
			}
			if raw[i] == 'n' {
				literal = "null"
			}
			for j := 0; j < len(literal); j++ {
				if i == len(raw) {
					return objectPrefixIncomplete
				}
				if raw[i] != literal[j] {
					return objectPrefixInvalid
				}
				i++
			}
		default:
			end, kind := prefixNumber(raw, i)
			if kind != objectPrefixComplete {
				return kind
			}
			i = end
		}
	}
	space()
	if i != len(raw) {
		return objectPrefixInvalid
	}
	return objectPrefixComplete
}
func prefixString(raw []byte, i int) (int, objectPrefixKind) {
	i++
	for i < len(raw) {
		c := raw[i]
		if c == '"' {
			return i + 1, objectPrefixComplete
		}
		if c < 0x20 {
			return i, objectPrefixInvalid
		}
		if c == '\\' {
			i++
			if i == len(raw) {
				return i, objectPrefixIncomplete
			}
			switch raw[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i++
			case 'u':
				i++
				for n := 0; n < 4; n++ {
					if i == len(raw) {
						return i, objectPrefixIncomplete
					}
					c = raw[i]
					if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
						return i, objectPrefixInvalid
					}
					i++
				}
			default:
				return i, objectPrefixInvalid
			}
			continue
		}
		if c >= utf8.RuneSelf {
			if !utf8.FullRune(raw[i:]) {
				return len(raw), objectPrefixIncomplete
			}
			r, n := utf8.DecodeRune(raw[i:])
			if r == utf8.RuneError && n == 1 {
				return i, objectPrefixInvalid
			}
			i += n
		} else {
			i++
		}
	}
	return i, objectPrefixIncomplete
}
func prefixNumber(raw []byte, i int) (int, objectPrefixKind) {
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	if raw[i] == '-' {
		i++
		if i == len(raw) {
			return i, objectPrefixIncomplete
		}
	}
	if raw[i] == '0' {
		i++
	} else {
		if raw[i] < '1' || raw[i] > '9' {
			return i, objectPrefixInvalid
		}
		for i < len(raw) && digit(raw[i]) {
			i++
		}
	}
	if i < len(raw) && raw[i] == '.' {
		i++
		if i == len(raw) {
			return i, objectPrefixIncomplete
		}
		if !digit(raw[i]) {
			return i, objectPrefixInvalid
		}
		for i < len(raw) && digit(raw[i]) {
			i++
		}
	}
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		i++
		if i == len(raw) {
			return i, objectPrefixIncomplete
		}
		if raw[i] == '+' || raw[i] == '-' {
			i++
			if i == len(raw) {
				return i, objectPrefixIncomplete
			}
		}
		if !digit(raw[i]) {
			return i, objectPrefixInvalid
		}
		for i < len(raw) && digit(raw[i]) {
			i++
		}
	}
	return i, objectPrefixComplete
}
