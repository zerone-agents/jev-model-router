// Package inference owns the request snapshot, planning, and best-effort record
// lifecycle shared by inference protocols. Wire encoding stays in transport.
package inference

import (
	"context"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type SnapshotStore interface {
	Snapshot(context.Context) (routing.Snapshot, error)
}
type Service struct {
	Fallback        FallbackConfig
	Store           SnapshotStore
	Planner         *routing.Planner
	Recorder        *routing.Recorder
	DecisionTimeout time.Duration
}
type Request struct {
	OnRetry      func() error
	snapshot     *routing.Snapshot
	options      routing.PlanOptions
	current      int
	diagnostics  []routing.AttemptDiagnostic
	service      *Service
	ctx          context.Context
	parent       context.Context
	cancelBudget context.CancelFunc
	record       routing.Record
}

func (s *Service) Begin(ctx context.Context, id string) *Request {
	return &Request{service: s, ctx: ctx, parent: ctx, record: routing.Record{DecisionUsage: &routing.DecisionUsage{Complete: true}, RequestID: id, CreatedAt: time.Now().UTC()}}
}
func (r *Request) Plan(req routing.Request, check func(routing.Target) error) (routing.Plan, error) {
	return r.PlanWithSummary(req, check, routing.RequestSummary(req))
}

// PlanWithSummary accepts a bounded summary extracted before protocol conversion.
func (r *Request) PlanWithSummary(req routing.Request, check func(routing.Target) error, summary string) (routing.Plan, error) {
	return r.PlanPrepared(req, check, nil, summary)
}

func (r *Request) PlanPrepared(req routing.Request, check func(routing.Target) error, estimate routing.PreparedEstimate, summary string) (routing.Plan, error) {
	r.record.Mode = "explicit"
	if req.Model == "auto" {
		r.record.Mode = "auto"
	}
	r.record.RequestSummary = summary
	if r.snapshot == nil {
		snapshot, err := r.service.Store.Snapshot(r.ctx)
		if err != nil {
			return routing.Plan{}, err
		}
		r.snapshot = &snapshot
	}
	snapshot := *r.snapshot
	r.record.ConfigVersion = snapshot.Version
	ctx, cancel := routing.WithStageTimeout(r.ctx, r.service.DecisionTimeout)
	defer cancel()
	start := time.Now()
	plan, err := r.service.Planner.PlanWithOptions(ctx, snapshot, req, check, estimate, r.options)
	r.record.DecisionMillis += time.Since(start).Milliseconds()
	u := plan.RecordedDecisionUsage()
	r.record.DecisionUsage.InputTokens += u.InputTokens
	r.record.DecisionUsage.OutputTokens += u.OutputTokens
	r.record.DecisionUsage.Invocations += u.Invocations
	r.record.DecisionUsage.Complete = r.record.DecisionUsage.Complete && u.Complete
	r.record.Path = plan.Path
	r.record.CandidateIDs = plan.CandidateIDs
	if plan.ModelID != "" {
		r.record.ModelID = plan.ModelID
	}
	if ctx.Err() == context.DeadlineExceeded {
		return plan, routing.Fail("timeout", "decision timed out")
	}
	if ctx.Err() != nil {
		return plan, routing.Fail("cancelled", "request cancelled")
	}
	return plan, err
}
func (r *Request) Finish(err error) {
	if r.cancelBudget != nil {
		defer r.cancelBudget()
	}
	r.record.Outcome = "success"
	if err != nil {
		r.record.Outcome = "error"
		r.record.ErrorCode = routingErrorCode(err)
	}
	r.service.Recorder.Save(r.record)
}

// Context remains alive through terminal response writes until Finish releases it.
func (r *Request) Context() context.Context { return r.ctx }

type RouteMetadata struct {
	Attempt        int                         `json:"attempt,omitempty"`
	Attempts       []routing.AttemptDiagnostic `json:"attempts,omitempty"`
	RequestID      string                      `json:"request_id"`
	ModelID        string                      `json:"model_id"`
	Path           string                      `json:"path"`
	ConfigVersion  int64                       `json:"config_version"`
	DecisionMillis int64                       `json:"decision_ms"`
}

func (r *Request) Metadata() RouteMetadata {
	return RouteMetadata{Attempt: r.current, Attempts: r.DiagnosticAttempts(), RequestID: r.record.RequestID, ModelID: r.record.ModelID, Path: r.record.Path, ConfigVersion: r.record.ConfigVersion, DecisionMillis: r.record.DecisionMillis}
}
