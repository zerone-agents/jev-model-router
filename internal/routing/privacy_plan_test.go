package routing

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPrivacyPlanRestrictsTargetsAndFallback(t *testing.T) {
	s := Snapshot{Privacy: PrivacyConfig{Enabled: true}, Providers: []Provider{{ID: "p"}}, Models: []Model{
		{ID: "local", ProviderID: "p", Location: "local", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}},
		{ID: "cloud", ProviderID: "p", Location: "cloud", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}},
	}}
	request := Request{Model: "auto", Messages: []Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}}
	planner := &Planner{}
	if _, err := planner.Plan(context.Background(), s, request); err == nil {
		t.Fatal("privacy bypassed without classification")
	}
	plan, err := planner.PlanWithOptions(context.Background(), s, request, nil, nil, PlanOptions{PrivacyChecked: true, LocalOnly: true})
	if err != nil || plan.ModelID != "local" {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	request.Model = "cloud"
	if _, err := planner.PlanWithOptions(context.Background(), s, request, nil, nil, PlanOptions{PrivacyChecked: true, LocalOnly: true}); err == nil {
		t.Fatal("explicit cloud accepted")
	}
	request.Model = "auto"
	if _, err := planner.PlanWithOptions(context.Background(), s, request, nil, nil, PlanOptions{PrivacyChecked: true, LocalOnly: true, ExcludedModelIDs: []string{"local"}}); err == nil {
		t.Fatal("fallback escaped local restriction")
	}
}
