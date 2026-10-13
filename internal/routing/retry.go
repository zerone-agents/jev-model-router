package routing

import "context"

type RetryContext struct {
	ModelID        string `json:"model_id"`
	Code           string `json:"code"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	UpstreamCode   string `json:"upstream_code,omitempty"`
}
type PlanOptions struct {
	ExcludedModelIDs []string
	Retry            *RetryContext
}

func (p *Planner) PlanWithOptions(ctx context.Context, s Snapshot, r Request, check func(Target) error, estimate PreparedEstimate, options PlanOptions) (Plan, error) {
	return p.plan(ctx, s, r, check, check != nil, estimate, options)
}
