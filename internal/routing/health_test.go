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
	}{{context.Canceled, ""}, {ErrGenerationCapacity, ""}, {&UpstreamError{Status: 403}, ""}, {&UpstreamError{Status: 403, ReportedCode: "permission_error"}, ""}, {&UpstreamError{Status: 403, ReportedCode: "access_terminated_error"}, health.Permanent}, {&UpstreamError{Status: 451}, ""}, {&UpstreamError{Status: 429}, health.Limited}, {&UpstreamError{Status: 401, ReportedCode: "invalid_api_key"}, health.Permanent}, {&UpstreamError{Status: 500}, health.Transient}} {
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

type trackedHealthCredential struct {
	id     string
	closed int
}

func (c *trackedHealthCredential) Identity() string { return c.id }
func (c *trackedHealthCredential) Close() error     { c.closed++; return nil }

func TestPreparedHealthEstimateReleasesCredentials(t *testing.T) {
	for _, failEstimate := range []bool{false, true} {
		s, r := planFixture()
		credentials := map[string]*trackedHealthCredential{}
		svc := health.New(healthMemory{[]health.Record{{ID: "a", View: health.View{State: health.Isolated}}}}, health.DefaultPolicy(), nil)
		p := Planner{Health: &HealthGate{Service: svc, Prepare: func(_ context.Context, target Target) (Target, error) {
			c := &trackedHealthCredential{id: target.Model.ID}
			credentials[c.id] = c
			target.Prepared = c
			return target, nil
		}}}
		plan, err := p.PlanPreparedWithEstimate(context.Background(), s, r, func(Target) error { return nil }, func(target Target) (ContextEstimate, bool, error) {
			if target.Model.ID != "b" {
				t.Fatal("isolated target estimated")
			}
			if failEstimate {
				return ContextEstimate{}, false, Fail("internal_error", "estimate failed")
			}
			return ContextEstimate{TotalTokens: 1}, false, nil
		})
		if (err != nil) != failEstimate {
			t.Fatal(err)
		}
		if credentials["a"].closed != 1 {
			t.Fatal("excluded credential not released")
		}
		if failEstimate {
			if credentials["b"].closed != 1 {
				t.Fatal("failed estimate credential not released")
			}
		} else {
			if plan.ModelID != "b" || credentials["b"].closed != 0 {
				t.Fatal("selected credential released early")
			}
			_ = plan.Close()
			if credentials["b"].closed != 1 {
				t.Fatal("selected credential not released")
			}
		}
	}
}

func TestRequestErrorCodePrecedesServerStatus(t *testing.T) {
	for _, code := range []string{"content_filter", "content_policy_violation", "context_length_exceeded", "invalid_request_error", "unsupported_parameter", "request_too_large"} {
		t.Run(code, func(t *testing.T) {
			got := HealthOutcome(&UpstreamError{Status: 500, ReportedCode: code})
			if got.Kind != "" || got.Success {
				t.Fatalf("request error must remain health-neutral and ineligible for retry: %+v", got)
			}
		})
	}
}
