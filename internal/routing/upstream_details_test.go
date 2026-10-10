package routing

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpstreamDetails(t *testing.T) {
	raw := `{"detail":"{\"title\":\"Typesafe is not available in your region.\",\"status\":451}","api_key":"private","nested":{"Authorization":"Basic xyz","message":"credential EXACT_VALUE Bearer abc https://user:pass@example.org/?key=xyz"}}`
	d := SanitizeUpstream(raw, "EXACT_VALUE")
	b, _ := json.Marshal(d)
	for _, secret := range []string{"private", "xyz", "EXACT_VALUE", "user:pass", "Bearer abc"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s: %s", secret, b)
		}
	}
	if !strings.Contains(string(b), "Typesafe is not available in your region.") {
		t.Fatal(string(b))
	}
	if _, ok := d.Body.(map[string]any)["detail"].(map[string]any); !ok {
		t.Fatal("nested JSON lost")
	}
}
func TestUpstreamDetailsBounds(t *testing.T) {
	for _, n := range []int{20000, 70000} {
		d := SanitizeUpstream(strings.Repeat("中", n))
		if !d.Truncated {
			t.Fatal("missing truncation marker")
		}
	}
	d := SanitizeUpstream(`<script>alert(1)</script> password=hunter2`)
	if strings.Contains(d.Body.(string), "hunter2") {
		t.Fatal(d)
	}
}

func TestMalformedErrorStillRedactsCredentials(t *testing.T) {
	for _, raw := range []string{`{"api_key":"private-value",broken`, "proxy Cookie: a=private-value; b=another-value", `https://user:private-value@example.org?token=another-value`} {
		b, _ := json.Marshal(SanitizeUpstream(raw))
		if strings.Contains(string(b), "private-value") || strings.Contains(string(b), "another-value") {
			t.Fatal(string(b))
		}
	}
}
