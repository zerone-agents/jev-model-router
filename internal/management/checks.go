package management

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"time"
)

type Checks struct {
	Store                              ConfigStore
	Planner                            *routing.Planner
	Generator                          routing.Generator
	DecisionTimeout, FirstEventTimeout time.Duration
}
type TestResult struct {
	OK             bool   `json:"ok"`
	Error          *Error `json:"error,omitempty"`
	DurationMillis int64  `json:"duration_ms"`
	ConfigVersion  int64  `json:"config_version"`
}

func (c *Checks) Inspect(ctx context.Context, r routing.Request) (routing.Plan, error) {
	s, e := c.Store.Snapshot(ctx)
	if e != nil {
		return routing.Plan{}, e
	}
	d := c.DecisionTimeout
	if d <= 0 {
		d = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return c.Planner.Plan(ctx, s, r)
}
func (c *Checks) TestModel(ctx context.Context, id string) (TestResult, error) {
	s, e := c.Store.Snapshot(ctx)
	if e != nil {
		return TestResult{}, e
	}
	var target routing.Target
	found := false
	for _, m := range s.Models {
		if m.ID == id {
			target.Model = m
			for _, p := range s.Providers {
				if p.ID == m.ProviderID {
					target.Provider = p
					found = true
				}
			}
			break
		}
	}
	if !found {
		return TestResult{}, routing.Fail("not_found", "model not found")
	}
	start := time.Now()
	d := c.FirstEventTimeout
	if d <= 0 {
		d = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_, e = c.Generator.Complete(ctx, target, provider.TestRequest())
	result := TestResult{OK: e == nil, ConfigVersion: s.Version, DurationMillis: time.Since(start).Milliseconds()}
	if e != nil {
		result.Error = Failure(e).Error
	}
	return result, nil
}
func (c *Checks) Register(s *Service) {
	s.Register("route.inspect", func(ctx context.Context, _ string, call Call) (Result, error) {
		var r routing.Request
		if json.Unmarshal(call.Input, &r) != nil {
			return Result{}, routing.Fail("invalid_request", "invalid request")
		}
		p, e := c.Inspect(ctx, r)
		if e != nil {
			return Result{}, e
		}
		return Success(p, time.Now()), nil
	})
	s.Register("models.test", func(ctx context.Context, _ string, call Call) (Result, error) {
		var v struct{ ID string }
		json.Unmarshal(call.Input, &v)
		r, e := c.TestModel(ctx, v.ID)
		if e != nil {
			return Result{}, e
		}
		return Success(r, time.Now()), nil
	})
}
