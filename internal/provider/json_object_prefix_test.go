package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestObjectPrefixBoundaries(t *testing.T) {
	for kind, values := range map[objectPrefixKind][]string{
		objectPrefixIncomplete: {``, ` `, `{`, `{"a":`, `{"a":"x\`, `{"a":"\u12`, `{"a":-`, `{"a":1.`, `{"a":1e`, `{"a":1e+`, `{"a":[{"b":tru`},
		objectPrefixComplete:   {`{}`, `{"a":9007199254740993}`, `{"a":1e100000}`, `{"a":-1.25e+12,"b":[true,null,{"x":"\\"}]}`},
		objectPrefixInvalid:    {`[]`, `null`, `{"a":01`, `{"a":+1`, `{"a":1e+-`, `{"a":"\q`, `{"a":truX`, `{"a":]`, `{"a":1,}`, `{}x`, `{} {}`, `{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `{"a":[1,]}`},
	} {
		for _, raw := range values {
			t.Run(raw, func(t *testing.T) {
				if got := classifyObjectPrefix([]byte(raw)); got != kind {
					t.Fatalf("got %d want %d", got, kind)
				}
			})
		}
	}
}
func TestObjectPrefixEscapesAndDepth(t *testing.T) {
	for _, raw := range []string{`{"a":"\"\\\/\b\f\n\r\t\u0000\uFFFF中文😀"}`, `{"nested":[{},[null,false,true,-0.01e-3]]}`} {
		for i := 0; i <= len(raw); i++ {
			if classifyObjectPrefix([]byte(raw[:i])) == objectPrefixInvalid {
				t.Fatalf("invalid valid prefix %q", raw[:i])
			}
		}
	}
	for _, raw := range []string{"{\"x\":\"\x01", "{\"x\":\"\xff", `{"x":"\u0g`, "{\"x\":\"\xe0\x80"} {
		if classifyObjectPrefix([]byte(raw)) != objectPrefixInvalid {
			t.Fatalf("invalid bytes accepted %q", raw)
		}
	}
	for _, depth := range []int{10000, 10001} {
		raw := `{"a":` + strings.Repeat("[", depth-1) + "0" + strings.Repeat("]", depth-1) + "}"
		got := classifyObjectPrefix([]byte(raw))
		if (got == objectPrefixComplete) != (depth == 10000) {
			t.Fatalf("depth %d: %d", depth, got)
		}
	}
}
func FuzzObjectPrefix(f *testing.F) {
	for _, raw := range []string{`{}`, `{"x":"中文😀"}`, `{"n":1e100000,"a":[true,null,{}]}`, `{"a":1,}`, `[]`, `{"a":1,"a":2}`} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		before := append([]byte(nil), raw...)
		kind := classifyObjectPrefix(raw)
		if !bytes.Equal(raw, before) {
			t.Fatal("mutation")
		}
		if kind == objectPrefixComplete && (!json.Valid(raw) || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{') {
			t.Fatal("invalid complete")
		}
		if uniqueJSON(raw) == nil && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) && len(raw) < 4096 {
			for i := 0; i <= len(raw); i++ {
				if classifyObjectPrefix(raw[:i]) == objectPrefixInvalid {
					t.Fatalf("invalid prefix %q", raw[:i])
				}
			}
		}
	})
}
