package routing

import (
	"context"
	"errors"
	"testing"
)

func TestPreparedEstimateFallback(t *testing.T) {
	s := Snapshot{Providers: []Provider{{ID: "p"}}, Models: []Model{{ID: "small", ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 100}}, {ID: "large", ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 200}}}}
	r := Request{Model: "auto", Messages: []Message{{Role: "user", Content: []byte(`"hi"`)}}}
	p := &Planner{}
	checked := map[string]bool{}
	check := func(t Target) error { checked[t.Model.ID] = true; return nil }
	estimate := func(target Target) (ContextEstimate, bool, error) {
		if !checked[target.Model.ID] {
			t.Fatal("estimate before check")
		}
		return ContextEstimate{Method: target.Model.ID, TotalTokens: 300}, false, nil
	}
	plan, err := p.PlanPreparedWithEstimate(context.Background(), s, r, check, estimate)
	if err != nil || plan.ModelID != "large" || plan.Path != "context_estimate_fallback" || plan.ContextEstimate.Method != "large" {
		t.Fatalf("%+v %v", plan, err)
	}
	_, err = p.PlanPreparedWithEstimate(context.Background(), s, r, check, func(Target) (ContextEstimate, bool, error) { return ContextEstimate{TotalTokens: 300}, true, nil })
	if err == nil {
		t.Fatal("exact overflow accepted")
	}
	r.Model = "small"
	_, err = p.PlanPreparedWithEstimate(context.Background(), s, r, check, func(Target) (ContextEstimate, bool, error) {
		t.Fatal("explicit estimate called")
		return ContextEstimate{}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("internal")
	_, err = p.PlanPreparedWithEstimate(context.Background(), s, r, func(Target) error { return sentinel }, estimate)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
