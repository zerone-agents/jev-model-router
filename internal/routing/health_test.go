package routing

import (
	"context"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"testing"
)

func TestHealthClassification(t *testing.T) {
	for _, tt := range []struct {
		e error
		k health.Kind
	}{{context.Canceled, ""}, {ErrGenerationCapacity, ""}, {&UpstreamError{Status: 403}, ""}, {&UpstreamError{Status: 451}, ""}, {&UpstreamError{Status: 429}, health.Limited}, {&UpstreamError{Status: 401, ReportedCode: "invalid_api_key"}, health.Permanent}, {&UpstreamError{Status: 500}, health.Transient}} {
		if got := HealthOutcome(tt.e); got.Kind != tt.k {
			t.Fatalf("%v: %s", tt.e, got.Kind)
		}
	}
}

type healthMemory struct{ rows []health.Record }

func (h healthMemory) LoadHealthKey(context.Context) ([32]byte, error)     { return [32]byte{1}, nil }
func (h healthMemory) LoadHealth(context.Context) ([]health.Record, error) { return h.rows, nil }
func (h healthMemory) SaveHealth(context.Context, health.Record) error     { return nil }

type testCredential string

func (c testCredential) Identity() string { return string(c) }
func (c testCredential) Close() error     { return nil }
func TestAutoExcludesIsolatedBeforeDecision(t *testing.T) {
	s, r := planFixture()
	svc := health.New(healthMemory{[]health.Record{{ID: "a", View: health.View{State: health.Isolated}}}}, health.DefaultPolicy(), nil)
	p := Planner{Health: &HealthGate{Service: svc, Prepare: func(_ context.Context, t Target) (Target, error) {
		t.Prepared = testCredential(t.Model.ID)
		return t, nil
	}}}
	plan, e := p.Plan(context.Background(), s, r)
	if e != nil || plan.ModelID != "b" || plan.Path != "single_candidate" {
		t.Fatalf("%+v %v", plan, e)
	}
	r.Model = "a"
	plan, e = p.Plan(context.Background(), s, r)
	if e != nil || plan.ModelID != "a" {
		t.Fatal("explicit blocked", e)
	}
}
func TestHealthExclusionsAreBounded(t *testing.T) {
	s, r := planFixture()
	s.Models = nil
	rows := []health.Record{}
	for n := 0; n < 205; n++ {
		id := fmt.Sprintf("m%d", n)
		s.Models = append(s.Models, Model{ID: id, ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}})
		rows = append(rows, health.Record{ID: health.TargetID(id), View: health.View{State: health.Isolated}})
	}
	p := Planner{Health: &HealthGate{Service: health.New(healthMemory{rows}, health.DefaultPolicy(), nil), Prepare: func(_ context.Context, t Target) (Target, error) {
		t.Prepared = testCredential(t.Model.ID)
		return t, nil
	}}}
	plan, e := p.Plan(context.Background(), s, r)
	if e == nil || len(plan.HealthExclusions) > 200 {
		t.Fatalf("unbounded health exclusions: %d", len(plan.HealthExclusions))
	}
}
