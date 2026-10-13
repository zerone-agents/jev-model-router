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

func TestExecuteFallbackStops(t *testing.T) {
	for _, mode := range []string{"explicit", "disabled", "zero-retries", "committed", "local", "cancelled", "two-failures", "budget"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultFallbackConfig()
			if mode == "disabled" {
				cfg.Enabled = false
			}
			if mode == "zero-retries" {
				cfg.MaxRetries = 0
			}
			if mode == "budget" {
				cfg.Timeout = 30 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &snapshotCounter{}
			svc := &Service{Store: store, Planner: &routing.Planner{Decider: firstDecider{}}, DecisionTimeout: time.Second, Fallback: cfg}
			r := svc.Begin(ctx, "one-request")
			defer r.Finish(nil)
			req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
			if mode == "explicit" {
				req.Model = "a"
			}
			calls := 0
			err := r.Execute(req, nil, nil, "hi", func(c context.Context, _ routing.Plan, n int) AttemptResult {
				calls++
				if mode == "cancelled" {
					cancel()
				}
				if mode == "budget" && n == 2 {
					stage, stop := routing.WithStageTimeout(c, time.Second)
					defer stop()
					<-stage.Done()
					if routing.HealthOutcome(context.Cause(stage)).Kind != "" {
						t.Fatal("remaining total budget counted as upstream failure")
					}
					return AttemptResult{Err: stage.Err(), RetryableGenerationFailure: true}
				}
				return AttemptResult{Err: routing.Fail("upstream_error", "unavailable"), RetryableGenerationFailure: mode != "local", Committed: mode == "committed"}
			})
			want := 1
			if mode == "two-failures" || mode == "budget" {
				want = 2
			}
			if err == nil || calls != want || len(r.Attempts()) != want || store.calls != 1 {
				t.Fatalf("err=%v calls=%d attempts=%d snapshots=%d", err, calls, len(r.Attempts()), store.calls)
			}
			if mode == "budget" && routing.Diagnose(err).Code != "timeout" {
				t.Fatalf("wrong budget error: %v", err)
			}
		})
	}
}

func TestRequestBudgetLivesThroughResponse(t *testing.T) {
	svc := &Service{Store: &snapshotCounter{}, Planner: &routing.Planner{Decider: firstDecider{}}, DecisionTimeout: time.Second, Fallback: DefaultFallbackConfig()}
	r := svc.Begin(context.Background(), "response")
	err := r.Execute(routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}, nil, nil, "hi", func(context.Context, routing.Plan, int) AttemptResult { return AttemptResult{} })
	if err != nil || r.Context().Err() != nil {
		t.Fatalf("budget ended before terminal response: %v", err)
	}
	r.Finish(err)
	if r.Context().Err() == nil {
		t.Fatal("Finish did not release the budget")
	}
}
