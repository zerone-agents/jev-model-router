package routing

import "encoding/json"

// AttemptSummary is safe to persist; detailed upstream bodies are response-only.
type AttemptSummary struct {
	DecisionUsage  *DecisionUsage `json:"decision_usage,omitempty"`
	Index          int            `json:"index"`
	Stage          string         `json:"stage"`
	ModelID        string         `json:"model_id,omitempty"`
	Path           string         `json:"path,omitempty"`
	DecisionMillis int64          `json:"decision_ms"`
	DurationMillis int64          `json:"duration_ms"`
	Outcome        string         `json:"outcome"`
	ErrorCode      string         `json:"error_code,omitempty"`
}
type AttemptDiagnostic struct {
	AttemptSummary
	Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
}
type AttemptError struct {
	Cause    error
	Attempts []AttemptDiagnostic
}

func (e *AttemptError) Error() string { return e.Cause.Error() }
func (e *AttemptError) Unwrap() error { return e.Cause }

func BoundAttempts(in []AttemptDiagnostic) []AttemptDiagnostic {
	if len(in) > 2 {
		in = in[:2]
	}
	out := append([]AttemptDiagnostic(nil), in...)
	for i := range out {
		if out[i].Diagnostic != nil {
			d := *out[i].Diagnostic
			d.Attempts = nil
			if d.UpstreamError != nil {
				u := *d.UpstreamError
				d.UpstreamError = &u
			}
			out[i].Diagnostic = &d
		}
	}
	for i := range out {
		b, _ := json.Marshal(out)
		if len(b) <= 30<<10 {
			break
		}
		if d := out[i].Diagnostic; d != nil && d.UpstreamError != nil {
			d.UpstreamError.Body = nil
			d.UpstreamError.Truncated = true
		}
	}
	return out
}
