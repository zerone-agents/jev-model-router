package failurecapture

import (
	"mime"
	"net/http"
)

// ProtocolHeaders retains finite protocol values and never copies arbitrary
// header values or media type parameters.
func ProtocolHeaders(headers http.Header) map[string]string {
	out := make(map[string]string)
	if media, _, err := mime.ParseMediaType(headers.Get("Content-Type")); err == nil {
		switch media {
		case "application/json", "text/event-stream":
			out["content_type"] = media
		}
	}
	if headers.Get("Anthropic-Version") == "2023-06-01" {
		out["anthropic_version"] = "2023-06-01"
	}
	switch headers.Get("Content-Encoding") {
	case "gzip", "identity":
		out["content_encoding"] = headers.Get("Content-Encoding")
	}
	return out
}
