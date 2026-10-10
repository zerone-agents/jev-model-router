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
	Store           SnapshotStore
	Planner         *routing.Planner
	Recorder        *routing.Recorder
	DecisionTimeout time.Duration
}
type Request struct {
	service *Service
	ctx     context.Context
	record  routing.Record
}

func (s *Service) Begin(ctx context.Context, id string) *Request {
	return &Request{service: s, ctx: ctx, record: routing.Record{RequestID: id, CreatedAt: time.Now().UTC()}}
}
func (r *Request) Plan(req routing.Request, check func(routing.Target) error) (routing.Plan, error) {
	return r.PlanWithSummary(req, check, routing.RequestSummary(req))
}

// PlanWithSummary accepts a bounded summary extracted before protocol conversion.
func (r *Request) PlanWithSummary(req routing.Request, check func(routing.Target) error, summary string) (routing.Plan, error) {
	r.record.Mode = "explicit"
	if req.Model == "auto" {
		r.record.Mode = "auto"
	}
	r.record.RequestSummary = summary
	snapshot, err := r.service.Store.Snapshot(r.ctx)
	if err != nil {
		return routing.Plan{}, err
	}
	r.record.ConfigVersion = snapshot.Version
	ctx, cancel := context.WithTimeout(r.ctx, r.service.DecisionTimeout)
	defer cancel()
	start := time.Now()
	var plan routing.Plan
	if check == nil {
		plan, err = r.service.Planner.Plan(ctx, snapshot, req)
	} else {
		plan, err = r.service.Planner.PlanPrepared(ctx, snapshot, req, check)
	}
	r.record.DecisionMillis = time.Since(start).Milliseconds()
	r.record.Path = plan.Path
	r.record.CandidateIDs = plan.CandidateIDs
	r.record.ModelID = plan.ModelID
	if ctx.Err() == context.DeadlineExceeded {
		return plan, routing.Fail("timeout", "decision timed out")
	}
	if ctx.Err() != nil {
		return plan, routing.Fail("cancelled", "request cancelled")
	}
	return plan, err
}
func (r *Request) Finish(err error) {
	r.record.Outcome = "success"
	if err != nil {
		r.record.Outcome = "error"
		r.record.ErrorCode = routingErrorCode(err)
	}
	r.service.Recorder.Save(r.record)
}

type RouteMetadata struct {
	RequestID      string `json:"request_id"`
	ModelID        string `json:"model_id"`
	Path           string `json:"path"`
	ConfigVersion  int64  `json:"config_version"`
	DecisionMillis int64  `json:"decision_ms"`
}

func (r *Request) Metadata() RouteMetadata {
	return RouteMetadata{RequestID: r.record.RequestID, ModelID: r.record.ModelID, Path: r.record.Path, ConfigVersion: r.record.ConfigVersion, DecisionMillis: r.record.DecisionMillis}
}
