package routing

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAttemptDiagnosticsBounded(t *testing.T) {
	d := Diagnostic{Code: "upstream_error", UpstreamError: &UpstreamDetails{Body: strings.Repeat("x", 20000)}}
	err := &AttemptError{Cause: Fail("fallback_exhausted", "no backup"), Attempts: []AttemptDiagnostic{{AttemptSummary: AttemptSummary{Index: 1}, Diagnostic: &d}, {AttemptSummary: AttemptSummary{Index: 2}, Diagnostic: &d}}}
	b, _ := json.Marshal(Diagnose(err))
	if len(b) > 32768 {
		t.Fatal(len(b))
	}
	if !strings.Contains(string(b), `"attempts"`) {
		t.Fatal(string(b))
	}
}
