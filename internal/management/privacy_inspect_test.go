package management_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type inspectClassifier struct{ calls int }

func (c *inspectClassifier) Classify(context.Context, routing.DecisionConfig, string, json.RawMessage) (routing.PrivacyDecision, error) {
	c.calls++
	return routing.PrivacyDecision{Outcome: routing.PrivacySensitive, Invoked: true, Usage: &routing.Usage{InputTokens: 4, OutputTokens: 1}}, nil
}
func TestPrivacyRouteInspectUsesLocalGateAndCountsClassification(t *testing.T) {
	checks, store, _ := checksFixture()
	store.s.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	store.s.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	store.s.Models[0].Enabled = true
	store.s.Models[0].Location = "local"
	cloud := store.s.Models[0]
	cloud.ID = "cloud"
	cloud.Location = "cloud"
	store.s.Models = append(store.s.Models, cloud)
	classifier := &inspectClassifier{}
	checks.Planner.PrivacyClassifier = classifier
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"secret"`)}}}
	plan, err := checks.Inspect(context.Background(), req)
	if err != nil || plan.ModelID != "m" || plan.PrivacyOutcome != routing.PrivacySensitive || plan.PrivacyDecisionUsage == nil || plan.PrivacyDecisionUsage.Invocations != 1 {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	req.Model = "cloud"
	if _, err := checks.Inspect(context.Background(), req); err == nil {
		t.Fatal("explicit cloud allowed")
	}
	if classifier.calls != 2 {
		t.Fatalf("classification calls %d", classifier.calls)
	}
}

func TestPrivacyRouteInspectManagementContract(t *testing.T) {
	checks, store, _ := checksFixture()
	store.s.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	store.s.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	store.s.Models[0].Enabled = true
	store.s.Models[0].Location = "local"
	classifier := &inspectClassifier{}
	checks.Planner.PrivacyClassifier = classifier
	svc := management.New(store, nil)
	checks.Register(svc)
	result, err := svc.Execute(context.Background(), "settings", management.Call{CapabilityID: "route.inspect", Input: json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hello"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Outcome string `json:"privacy_outcome"`
		Usage   struct {
			Invocations int  `json:"invocations"`
			Complete    bool `json:"complete"`
		} `json:"privacy_decision_usage"`
	}
	if err := json.Unmarshal(result.Data, &got); err != nil || got.Outcome != "sensitive" || got.Usage.Invocations != 1 || !got.Usage.Complete {
		t.Fatalf("result %s err %v", result.Data, err)
	}
}
