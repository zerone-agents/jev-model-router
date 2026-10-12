package app

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"sync"
	"time"
)

type healthRuntime struct {
	nextMaintenance time.Time
	store           management.ConfigStore
	gate            *routing.HealthGate
	generator       routing.Generator
	limits          health.Config
	probes          *health.ProbeLimiter
	wg              sync.WaitGroup
}

func (h *healthRuntime) target(ctx context.Context, id string) (routing.Target, int64, error) {
	s, e := h.store.Snapshot(ctx)
	if e != nil {
		return routing.Target{}, 0, e
	}
	for _, m := range s.Models {
		if m.ID == id {
			for _, p := range s.Providers {
				if p.ID == m.ProviderID {
					return routing.Target{Model: m, Provider: p}, s.Version, nil
				}
			}
		}
	}
	return routing.Target{}, s.Version, routing.Fail("not_found", "model not found")
}
func (h *healthRuntime) test(ctx context.Context, id string, automatic bool) (management.TestResult, error) {
	t, version, e := h.target(ctx, id)
	if e != nil {
		return management.TestResult{}, e
	}
	t, e = h.gate.PrepareTarget(ctx, t)
	if e != nil {
		return management.TestResult{}, e
	}
	if t.Prepared != nil {
		defer t.Prepared.Close()
	}
	before, e := h.gate.Service.Get(t.HealthIdentity())
	if e != nil {
		return management.TestResult{}, routing.Fail("health_unavailable", "health state unavailable")
	}
	if automatic && (!t.Model.Enabled || before.State != health.Isolated || before.NextProbe == nil || time.Now().Before(*before.NextProbe)) {
		return management.TestResult{}, nil
	}
	release, e := h.probes.Acquire(t.HealthIdentity())
	if e != nil {
		return management.TestResult{}, &routing.Error{Code: "health_probe_in_progress", Message: "A model check is in progress or probe capacity is full", Retryable: true}
	}
	defer release()
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, h.limits.Timeout)
	defer cancel()
	req := provider.TestRequest()
	req.Options = map[string]json.RawMessage{"max_completion_tokens": json.RawMessage(jsonNumber(h.limits.MaxTokens))}
	_, e = h.generator.Complete(ctx, t, req)
	// Ignored probe failures still need a future retry; never manufacture failure observations.
	after, _ := h.gate.Service.Get(t.HealthIdentity())
	if e != nil && after.State == health.Isolated && after.AppliedSequence == before.AppliedSequence {
		save, c := context.WithTimeout(context.Background(), time.Second)
		_ = h.gate.Service.DeferProbe(save, t.HealthIdentity(), before.AppliedSequence)
		c()
		after, _ = h.gate.Service.Get(t.HealthIdentity())
	}
	v := after.View
	persisted := !h.gate.Service.Warning()
	result := management.TestResult{OK: e == nil, ConfigVersion: version, DurationMillis: time.Since(start).Milliseconds(), Health: &v, Persisted: &persisted}
	if e != nil {
		result.Error = management.Failure(e).Error
	}
	return result, nil
}
func jsonNumber(n int) string { b, _ := json.Marshal(n); return string(b) }
func (h *healthRuntime) tick(ctx context.Context) {
	if time.Now().After(h.nextMaintenance) {
		h.maintain(ctx)
		h.nextMaintenance = time.Now().Add(time.Minute)
	}
	s, e := h.store.Snapshot(ctx)
	if e != nil {
		return
	}
	for _, m := range s.Models {
		if !m.Enabled {
			continue
		}
		for _, p := range s.Providers {
			if p.ID != m.ProviderID {
				continue
			}
			t, e := h.gate.PrepareTarget(ctx, routing.Target{Model: m, Provider: p})
			if e != nil {
				continue
			}
			r, e := h.gate.Service.Get(t.HealthIdentity())
			if t.Prepared != nil {
				t.Prepared.Close()
			}
			if e == nil && !h.probes.Active(t.HealthIdentity()) && r.State == health.Isolated && r.NextProbe != nil && !time.Now().Before(*r.NextProbe) {
				// One start per tick; concurrent work is bounded by ProbeLimiter.
				h.wg.Add(1)
				go func(id string) { defer h.wg.Done(); _, _ = h.test(ctx, id, true) }(m.ID)
				return
			}
		}
	}
}

func (h *healthRuntime) maintain(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s, e := h.store.Snapshot(ctx)
	if e != nil {
		return
	}
	current := map[health.TargetID]bool{}
	for _, m := range s.Models {
		for _, p := range s.Providers {
			if p.ID == m.ProviderID {
				t, e := h.gate.PrepareTarget(ctx, routing.Target{Model: m, Provider: p})
				if e != nil || t.Prepared == nil {
					return
				}
				current[t.HealthIdentity()] = true
				t.Prepared.Close()
			}
		}
	}
	h.gate.Service.Maintain(ctx, current)
}
