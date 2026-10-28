package inference

import (
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func (r *Request) StreamError(e error) error {
	d := routing.Diagnose(e)
	a := routing.AttemptDiagnostic{AttemptSummary: routing.AttemptSummary{EffectiveEffort: r.record.EffectiveEffort, Index: r.current, Stage: "generation", ModelID: r.record.ModelID, Path: r.record.Path, Outcome: "error", ErrorCode: d.Code}, Diagnostic: &d}
	return &routing.AttemptError{Cause: e, Attempts: routing.BoundAttempts(append(r.DiagnosticAttempts(), a))}
}
func (r *Request) ErrorStage() string {
	if n := len(r.record.Attempts); n > 0 {
		return r.record.Attempts[n-1].Stage
	}
	return "routing"
}
func (c FallbackConfig) Status() map[string]any {
	return map[string]any{"enabled": c.Enabled && c.MaxRetries == 1, "max_retries": c.MaxRetries, "timeout_seconds": int(c.Timeout.Seconds())}
}
