package inference

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"time"
)

type AttemptResult struct {
	Err                        error
	Committed                  bool
	RetryableGenerationFailure bool
}
type AttemptRunner func(context.Context, routing.Plan, int) AttemptResult
type AttemptSummary = routing.AttemptSummary

func (r *Request) Attempts() []AttemptSummary {
	return append([]AttemptSummary(nil), r.record.Attempts...)
}
func (r *Request) Execute(req routing.Request, check func(routing.Target) error, estimate routing.PreparedEstimate, summary string, run AttemptRunner) error {
	cfg := r.service.Fallback
	enabled := req.Model == "auto" && cfg.Enabled && cfg.MaxRetries == 1
	if enabled {
		ctx, cancel := context.WithDeadlineCause(r.ctx, r.record.CreatedAt.Add(cfg.Timeout), routing.ErrRequestBudgetExhausted)
		defer cancel()
		r.ctx = ctx
	}
	var excluded []string
	var retry *routing.RetryContext
	for index := 1; index <= 2; index++ {
		if e := context.Cause(r.ctx); e != nil {
			return r.wrap(terminalError(r.ctx, e))
		}
		start := time.Now()
		r.options = routing.PlanOptions{ExcludedModelIDs: excluded, Retry: retry}
		r.current = index
		before := r.record.DecisionMillis
		plan, err := r.PlanPrepared(req, check, estimate, summary)
		a := AttemptSummary{Index: index, Stage: "routing", ModelID: plan.ModelID, Path: plan.Path, DecisionMillis: r.record.DecisionMillis - before, Outcome: "error"}
		result := AttemptResult{Err: err}
		if err == nil {
			a.Stage = "generation"
			result = run(r.ctx, plan, index)
		}
		plan.Close()
		result.Err = terminalError(r.ctx, result.Err)
		if result.Err != nil && index > 1 {
			var e *routing.Error
			if errors.As(result.Err, &e) && e.Code == "no_candidates" {
				result.Err = routing.Fail("fallback_exhausted", "No eligible backup model is available")
			}
		}
		a.DurationMillis = time.Since(start).Milliseconds()
		d := routing.AttemptDiagnostic{AttemptSummary: a}
		if result.Err == nil {
			a.Outcome = "success"
			d.AttemptSummary = a
		} else {
			diag := routing.Diagnose(result.Err)
			a.ErrorCode = diag.Code
			d.AttemptSummary = a
			d.Diagnostic = &diag
		}
		r.record.Attempts = append(r.record.Attempts, a)
		r.diagnostics = append(r.diagnostics, d)
		if result.Err == nil {
			return nil
		}
		if !enabled || index == 2 || err != nil || result.Committed || !result.RetryableGenerationFailure || context.Cause(r.ctx) != nil {
			return r.wrap(result.Err)
		}
		diag := routing.Diagnose(result.Err)
		retry = &routing.RetryContext{ModelID: plan.ModelID, Code: diag.Code, UpstreamStatus: diag.UpstreamStatus, UpstreamCode: diag.UpstreamCode}
		excluded = append(excluded, plan.ModelID)
	}
	return routing.Fail("internal_error", "attempt limit exceeded")
}
func terminalError(ctx context.Context, e error) error {
	cause := context.Cause(ctx)
	if errors.Is(cause, routing.ErrRequestBudgetExhausted) || errors.Is(cause, context.DeadlineExceeded) {
		return routing.Fail("timeout", "request timed out")
	}
	if cause != nil {
		return routing.Fail("cancelled", "request cancelled")
	}
	return e
}
func (r *Request) wrap(e error) error {
	if e == nil {
		return nil
	}
	return &routing.AttemptError{Cause: e, Attempts: r.DiagnosticAttempts()}
}
func (r *Request) DiagnosticAttempts() []routing.AttemptDiagnostic {
	return append([]routing.AttemptDiagnostic(nil), r.diagnostics...)
}
