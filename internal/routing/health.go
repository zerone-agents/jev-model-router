package routing

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"io"
	"net"
	"time"
)

func HealthOutcome(err error) health.Outcome {
	if err == nil {
		return health.Outcome{Success: true}
	}
	if errors.Is(err, ErrRequestBudgetExhausted) || errors.Is(err, context.Canceled) || errors.Is(err, ErrGenerationCapacity) {
		return health.Outcome{}
	}
	var transport *TransportFailure
	if errors.As(err, &transport) {
		return health.Outcome{Kind: health.Transient}
	}
	var u *UpstreamError
	if errors.As(err, &u) {
		o := health.Outcome{Status: u.Status, Code: u.ReportedCode, RetryAt: health.ParseRetryAfter(u.RetryAfter, time.Now())}
		switch u.ReportedCode {
		case "billing_error", "invalid_api_key", "authentication_error", "insufficient_quota", "quota_exceeded", "model_not_found", "insufficient_balance":
			o.Kind = health.Permanent
			return o
		case "rate_limit_error", "rate_limit_exceeded":
			o.Kind = health.Limited
			return o
		case "content_filter", "content_policy_violation", "context_length_exceeded", "invalid_request_error", "unsupported_parameter":
			return health.Outcome{}
		}
		if u.Status == 429 {
			o.Kind = health.Limited
		} else if u.Status >= 500 && u.Status <= 599 {
			o.Kind = health.Transient
		}
		return o
	}
	var r *Error
	if errors.As(err, &r) {
		if r.Code == "timeout" {
			return health.Outcome{Kind: health.Transient}
		}
		return health.Outcome{}
	}
	var n net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &n) {
		return health.Outcome{Kind: health.Transient}
	}
	return health.Outcome{}
}

type HealthExclusion struct {
	ModelID   string       `json:"model_id"`
	State     health.State `json:"state"`
	Reason    string       `json:"reason"`
	NextProbe *time.Time   `json:"next_probe_at"`
}
type HealthGate struct {
	Service *health.Service
	Prepare func(context.Context, Target) (Target, error)
}

func (g *HealthGate) PrepareTarget(ctx context.Context, t Target) (Target, error) {
	return g.Prepare(ctx, t)
}
func (t Target) HealthIdentity() health.TargetID {
	if t.Prepared == nil {
		return ""
	}
	return health.TargetID(t.Prepared.Identity())
}
func (p Plan) Close() error {
	if p.Target.Prepared != nil {
		return p.Target.Prepared.Close()
	}
	return nil
}
