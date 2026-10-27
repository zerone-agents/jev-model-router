package inference

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
	"time"
)

type effortStore struct{}

func (effortStore) Snapshot(ctx context.Context) (routing.Snapshot, error) {
	s, _ := (&snapshotCounter{}).Snapshot(ctx)
	s.Models[0].AutoEffort = &routing.AutoEffortConfig{Allowed: []string{"low", "high"}}
	s.Models[1].AutoEffort = &routing.AutoEffortConfig{Allowed: []string{"low", "medium"}}
	return s, nil
}

type effortDecider struct {
	calls int
	t     *testing.T
}

func (d *effortDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	d.calls++
	if !in.SelectEffort || in.Request.Options["reasoning_effort"] != nil {
		d.t.Fatal("injected effort became caller input")
	}
	effort := "xhigh"
	if d.calls == 2 {
		effort = "low"
	}
	return routing.Decision{ModelID: in.Candidates[0].ID, Effort: effort}, nil
}
func TestFallbackEffortIsAttemptLocal(t *testing.T) {
	d := &effortDecider{t: t}
	svc := Service{Store: effortStore{}, Planner: &routing.Planner{Decider: d}, DecisionTimeout: time.Second, Fallback: DefaultFallbackConfig()}
	r := svc.Begin(context.Background(), "effort")
	defer r.Finish(nil)
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}, Options: map[string]json.RawMessage{}}
	err := r.Execute(req, nil, nil, "hi", func(_ context.Context, p routing.Plan, n int) AttemptResult {
		want := "high"
		if n == 2 {
			want = "low"
		}
		if p.EffectiveEffort != want {
			t.Fatalf("attempt %d got %s", n, p.EffectiveEffort)
		}
		if got := r.Metadata().EffectiveEffort; got != want {
			t.Fatalf("attempt %d metadata effort = %q, want %q", n, got, want)
		}
		copy := routing.WithEffectiveEffort(req, p.EffectiveEffort)
		copy.Options["reasoning_effort"] = json.RawMessage(`"max"`)
		if n == 1 {
			return AttemptResult{Err: routing.Fail("timeout", "slow"), RetryableGenerationFailure: true}
		}
		return AttemptResult{}
	})
	if r.record.EffectiveEffort != "low" || len(r.record.Attempts) != 2 ||
		r.record.Attempts[0].EffectiveEffort != "high" || r.record.Attempts[1].EffectiveEffort != "low" {
		t.Fatalf("incorrect recorded efforts: %+v", r.record)
	}
	if err != nil || d.calls != 2 || len(req.Options) != 0 {
		t.Fatalf("%v %d %+v", err, d.calls, req)
	}
}

func TestGenerationWithoutEffortClearsRecordedValue(t *testing.T) {
	svc := Service{Store: &snapshotCounter{}, Planner: &routing.Planner{Decider: firstDecider{}}, DecisionTimeout: time.Second}
	r := svc.Begin(context.Background(), "unset")
	r.record.EffectiveEffort = "high"
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}}
	err := r.Execute(req, nil, nil, "hello", func(context.Context, routing.Plan, int) AttemptResult { return AttemptResult{} })
	if err != nil || r.record.EffectiveEffort != "" || r.record.Attempts[0].EffectiveEffort != "" {
		t.Fatalf("stale effort: %+v, %v", r.record, err)
	}
}
