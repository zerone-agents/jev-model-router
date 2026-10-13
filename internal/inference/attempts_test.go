package inference

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
	"time"
)

type snapshotCounter struct{ calls int }

func (s *snapshotCounter) Snapshot(context.Context) (routing.Snapshot, error) {
	s.calls++
	return routing.Snapshot{Version: 7, Providers: []routing.Provider{{ID: "p"}}, Models: []routing.Model{{ID: "a", ProviderID: "p", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 8000}}, {ID: "b", ProviderID: "p", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 8000}}}, Decision: routing.DecisionConfig{Model: "jev"}}, nil
}

type firstDecider struct{}

func (firstDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	return routing.Decision{ModelID: in.Candidates[0].ID}, nil
}
func TestExecuteAutoFallback(t *testing.T) {
	s := &snapshotCounter{}
	svc := &Service{Store: s, Planner: &routing.Planner{Decider: firstDecider{}}, DecisionTimeout: time.Second, Fallback: DefaultFallbackConfig()}
	r := svc.Begin(context.Background(), "r")
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	var ids []string
	e := r.Execute(req, nil, nil, "hi", func(_ context.Context, p routing.Plan, n int) AttemptResult {
		ids = append(ids, p.ModelID)
		if n == 1 {
			return AttemptResult{Err: routing.Fail("timeout", "slow"), RetryableGenerationFailure: true}
		}
		return AttemptResult{}
	})
	if e != nil || s.calls != 1 || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("%v %v snapshots=%d", ids, e, s.calls)
	}
}
