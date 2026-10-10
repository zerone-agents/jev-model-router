package routing

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const upstreamReadLimit = 64 << 10
const upstreamDisplayLimit = 16 << 10

// UpstreamDetails is sanitized at the adapter boundary; never store raw requests
// or SDK diagnostics here. Body preserves JSON structure, or contains plain text.
type UpstreamDetails struct {
	RequestID string `json:"request_id,omitempty"`
	Body      any    `json:"body"`
	Truncated bool   `json:"truncated"`
}

var sensitiveField = regexp.MustCompile(`(?i)^(authorization|proxyauthorization|.*apikey|.*secret.*|.*password.*|.*credential.*|accesstoken|refreshtoken|idtoken|token|cookie|setcookie|signature|messages|prompt|input|request|requestbody|headers)$`)
var authText = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[a-z0-9._~+/=:-]+`)
var credentialText = regexp.MustCompile(`(?i)\b(api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|password|secret|authorization|cookie|set-cookie)["']?\s*[:=]\s*("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;<>]+)`)
var cookieText = regexp.MustCompile(`(?i)\b(?:set-cookie|cookie)\s*:\s*[^\r\n]+`)
var keyText = regexp.MustCompile(`\b(sk-[A-Za-z0-9._-]+|apikey_[A-Za-z0-9._-]+)\b`)
var urlText = regexp.MustCompile(`https?://[^\s<>"']+`)

// SanitizeUpstream accepts only an actual response body. Oversized input is
// omitted rather than exposing an incomplete credential at a read boundary.
func SanitizeUpstream(raw any, secrets ...string) *UpstreamDetails {
	if raw == nil {
		return nil
	}
	var b []byte
	switch v := raw.(type) {
	case string:
		b = []byte(v)
	case []byte:
		b = v
	case json.RawMessage:
		b = v
	default:
		b, _ = json.Marshal(raw)
	}
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if len(b) > upstreamReadLimit {
		return &UpstreamDetails{Body: "[upstream error body omitted: exceeds 64 KiB]", Truncated: true}
	}
	var value any
	if json.Unmarshal(b, &value) != nil {
		value = strings.ToValidUTF8(string(b), "�")
	}
	redact := func(s string) string {
		for _, secret := range secrets {
			if secret == "" {
				continue
			}
			encoded, _ := json.Marshal(secret)
			for _, variant := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret), base64.StdEncoding.EncodeToString([]byte(secret)), string(encoded[1 : len(encoded)-1])} {
				s = strings.ReplaceAll(s, variant, "[REDACTED]")
			}
		}
		s = cookieText.ReplaceAllString(s, "Cookie: [REDACTED]")
		s = authText.ReplaceAllString(s, "$1 [REDACTED]")
		s = credentialText.ReplaceAllString(s, "$1=[REDACTED]")
		s = keyText.ReplaceAllString(s, "[REDACTED]")
		return urlText.ReplaceAllStringFunc(s, func(v string) string {
			u, err := url.Parse(v)
			if err != nil {
				return "[REDACTED URL]"
			}
			if u.User != nil {
				u.User = url.User("REDACTED")
			}
			if u.RawQuery != "" {
				u.RawQuery = "REDACTED"
			}
			u.Fragment = ""
			return u.String()
		})
	}
	var clean func(any, int) any
	clean = func(v any, depth int) any {
		if depth > 32 {
			return "[omitted: nesting limit]"
		}
		switch x := v.(type) {
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, item := range x {
				name := strings.NewReplacer("-", "", "_", "", " ", "").Replace(k)
				if sensitiveField.MatchString(name) {
					out[redact(k)] = "[REDACTED]"
				} else {
					out[redact(k)] = clean(item, depth+1)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = clean(item, depth+1)
			}
			return out
		case string:
			var nested any
			if json.Unmarshal([]byte(x), &nested) == nil {
				switch nested.(type) {
				case map[string]any, []any:
					return clean(nested, depth+1)
				}
			}
			return redact(x)
		default:
			return v
		}
	}
	value = clean(value, 0)
	encoded, _ := json.Marshal(value)
	if len(encoded) > upstreamDisplayLimit {
		text := string(encoded)
		if s, ok := value.(string); ok {
			text = s
		}
		if len(text) > upstreamDisplayLimit {
			text = text[:upstreamDisplayLimit]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		return &UpstreamDetails{Body: text, Truncated: true}
	}
	return &UpstreamDetails{Body: value}
}

// SetRequestID accepts bounded identifier syntax, never arbitrary header text.
func (d *UpstreamDetails) SetRequestID(id string, secrets ...string) {
	if d == nil || len(id) == 0 || len(id) > 128 {
		return
	}
	for _, c := range id {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c) {
			continue
		}
		return
	}
	encoded, _ := json.Marshal(id)
	clean := SanitizeUpstream(encoded, secrets...)
	if clean != nil && clean.Body == id {
		d.RequestID = id
	}
}
