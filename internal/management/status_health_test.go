package management_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type statusHealthStore struct{ failSave bool }

func (s *statusHealthStore) LoadHealthKey(context.Context) ([32]byte, error)     { return [32]byte{}, nil }
func (s *statusHealthStore) LoadHealth(context.Context) ([]health.Record, error) { return nil, nil }
func (s *statusHealthStore) SaveHealth(context.Context, health.Record) error {
	if s.failSave {
		return errors.New("storage unavailable")
	}
	return nil
}

type statusCredential string

func (c statusCredential) Identity() string { return string(c) }
func (c statusCredential) Close() error     { return nil }

func TestStatusGetHealthyModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		models   []routing.Model
		healthy  []string
		missing  string
		failSave bool
		want     *int
		warning  bool
	}{
		{name: "known zero", models: []routing.Model{{ID: "unknown", ProviderID: "p", Enabled: true}}, want: intPtr(0)},
		{name: "disabled excluded", models: []routing.Model{{ID: "enabled", ProviderID: "p", Enabled: true}, {ID: "disabled", ProviderID: "p"}}, healthy: []string{"enabled", "disabled"}, want: intPtr(1)},
		{name: "partial unavailable", models: []routing.Model{{ID: "healthy", ProviderID: "p", Enabled: true}, {ID: "missing", ProviderID: "p", Enabled: true}}, healthy: []string{"healthy"}, missing: "missing", warning: true},
		{name: "persistence warning with valid count", models: []routing.Model{{ID: "healthy", ProviderID: "p", Enabled: true}}, healthy: []string{"healthy"}, failSave: true, want: intPtr(1), warning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			hs := health.New(&statusHealthStore{failSave: tc.failSave}, health.DefaultPolicy(), nil)
			for _, id := range tc.healthy {
				_ = hs.Observe(ctx, health.TargetID(id), hs.Begin(health.TargetID(id)), health.Outcome{Success: true})
			}
			store := &memoryStore{s: routing.Snapshot{Models: tc.models, Providers: []routing.Provider{{ID: "p"}}}}
			svc := management.New(store, nil)
			svc.Health = &routing.HealthGate{Service: hs, Prepare: func(_ context.Context, target routing.Target) (routing.Target, error) {
				if target.Model.ID == tc.missing {
					return target, errors.New("unavailable")
				}
				target.Prepared = statusCredential(target.Model.ID)
				return target, nil
			}}
			result, err := svc.Execute(ctx, "settings", management.Call{CapabilityID: "status.get", Input: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			var data struct {
				Healthy *int `json:"healthy_models"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}
			if (data.Healthy == nil) != (tc.want == nil) || (tc.want != nil && *data.Healthy != *tc.want) {
				t.Fatalf("healthy_models=%v, want %v", data.Healthy, tc.want)
			}
			warning := false
			for _, w := range result.Warnings {
				if w == "health_degraded" {
					warning = true
				}
			}
			if warning != tc.warning {
				t.Fatalf("warnings=%v, want health_degraded=%v", result.Warnings, tc.warning)
			}
		})
	}
}
func intPtr(v int) *int { return &v }
