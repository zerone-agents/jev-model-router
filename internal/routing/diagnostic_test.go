package routing

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDiagnosticSafeClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{401, "upstream_authentication"}, {402, "upstream_quota"}, {403, "upstream_permission"}, {429, "upstream_rate_limit"}, {503, "upstream_unavailable"}, {504, "timeout"}, {400, "upstream_invalid_request"}} {
		e := fmt.Errorf("wrapped: %w", &UpstreamError{Status: tc.status, Body: map[string]any{"message": "SECRET https://internal/", "code": "SECRET"}})
		d := Diagnose(e)
		b, _ := json.Marshal(d)
		if d.Code != tc.code || d.UpstreamStatus != tc.status || strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "internal/") {
			t.Fatal(string(b))
		}
	}
	code := "insufficient_quota"
	d := Diagnose(&UpstreamError{Status: 429, Body: map[string]any{"code": &code}})
	if d.Code != "upstream_quota" || d.UpstreamCode != code {
		t.Fatal(d)
	}
}
