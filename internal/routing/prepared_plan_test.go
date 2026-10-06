package routing

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type preparedDecider struct{ calls atomic.Int32 }

func (d *preparedDecider) Choose(_ context.Context, _ DecisionConfig, in DecisionInput) (Decision, error) {
	d.calls.Add(1)
	for _, m := range in.Candidates {
		if m.ID == "bad" {
			return Decision{}, errors.New("incompatible target reached decision")
		}
	}
	return Decision{ModelID: in.Candidates[0].ID}, nil
}
func preparedSnapshot() Snapshot {
	s := Snapshot{Providers: []Provider{{ID: "p"}}, Decision: DecisionConfig{Model: "jev"}}
	for _, id := range []string{"bad", "good", "other"} {
		s.Models = append(s.Models, Model{ID: id, ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}})
	}
	return s
}
func preparedRequest(model string) Request {
	return Request{Model: model, Messages: []Message{{Role: "user", Content: json.RawMessage(`"query"`)}}}
}
func TestPlanPreparedFiltersBeforeDecision(t *testing.T) {
	d := &preparedDecider{}
	p := &Planner{Decider: d, PrepareCheck: func(Request) func(Target) error { t.Fatal("default checker used"); return nil }}
	check := func(target Target) error {
		if target.Model.ID == "bad" {
			return Fail("unsupported_request", "incompatible")
		}
		return nil
	}
	plan, err := p.PlanPrepared(context.Background(), preparedSnapshot(), preparedRequest("auto"), check)
	if err != nil || len(plan.CandidateIDs) != 2 || d.calls.Load() != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
	_, err = p.PlanPrepared(context.Background(), preparedSnapshot(), preparedRequest("bad"), check)
	if err == nil || d.calls.Load() != 1 {
		t.Fatal("explicit check bypassed")
	}
	_, err = p.PlanPrepared(context.Background(), preparedSnapshot(), preparedRequest("auto"), func(Target) error { return Fail("unsupported_request", "all incompatible") })
	var known *Error
	if !errors.As(err, &known) || known.Code != "no_candidates" || d.calls.Load() != 1 {
		t.Fatal(err)
	}
}
func TestPlanPreparedInternalFailure(t *testing.T) {
	p := &Planner{}
	for _, check := range []func(Target) error{nil, func(Target) error { return errors.New("broken conversion") }, func(Target) error { return Fail("internal_error", "broken") }} {
		_, err := p.PlanPrepared(context.Background(), preparedSnapshot(), preparedRequest("auto"), check)
		if err == nil || (func() bool { var known *Error; return errors.As(err, &known) && known.Code == "no_candidates" })() {
			t.Fatal(err)
		}
	}
}
func TestPlanPreparedFallbackIsolation(t *testing.T) {
	p := &Planner{Estimate: func(Model, Request) (int64, bool, error) { return 100000, false, nil }, Check: func(Target, Request) error { return Fail("unsupported_request", "OpenAI check") }}
	s := preparedSnapshot()
	s.Models[0].Capabilities.ContextLimit = 99999
	s.Models = s.Models[:2]
	check := func(target Target) error {
		if target.Model.ID == "bad" {
			return Fail("unsupported_request", "bad")
		}
		return nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := p.PlanPrepared(context.Background(), s, preparedRequest("auto"), check)
			if err != nil || plan.ModelID != "good" || plan.Path != "context_estimate_fallback" {
				t.Errorf("%+v %v", plan, err)
			}
			if _, err = p.Plan(context.Background(), s, preparedRequest("auto")); err == nil {
				t.Error("default check bypassed")
			}
		}()
	}
	wg.Wait()
}
