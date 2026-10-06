package routing

import (
	"context"
	"encoding/json"
	"testing"
)

type chooseFunc func(context.Context, DecisionConfig, DecisionInput) (Decision, error)

func (f chooseFunc) Choose(c context.Context, d DecisionConfig, i DecisionInput) (Decision, error) {
	return f(c, d, i)
}
func planFixture() (Snapshot, Request) {
	return Snapshot{Version: 3, Prompt: "balanced", Decision: DecisionConfig{Model: "jev"}, Providers: []Provider{{ID: "p"}}, Models: []Model{{ID: "a", ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}}, {ID: "b", ProviderID: "p", Enabled: true, Capabilities: Capabilities{ContextLimit: 10000}}}}, Request{Model: "auto", Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
}
func counterPlanner(calls *int) *Planner {
	return &Planner{Decider: chooseFunc(func(_ context.Context, _ DecisionConfig, in DecisionInput) (Decision, error) {
		*calls++
		return Decision{ModelID: in.Candidates[0].ID}, nil
	})}
}
func TestExplicitSkipsJev(t *testing.T) {
	s, r := planFixture()
	r.Model = "b"
	calls := 0
	p, e := counterPlanner(&calls).Plan(context.Background(), s, r)
	if e != nil || calls != 0 || p.Target.Model.ID != "b" || p.Path != "explicit" {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestAutoZeroOneMany(t *testing.T) {
	for n := 0; n < 3; n++ {
		s, r := planFixture()
		s.Models = s.Models[:n]
		calls := 0
		p, e := counterPlanner(&calls).Plan(context.Background(), s, r)
		if n == 0 {
			if e == nil {
				t.Fatal("zero accepted")
			}
			continue
		}
		if e != nil || p.Target.Model.ID != "a" || calls != n-1 {
			t.Fatalf("n=%d %+v %v", n, p, e)
		}
	}
}
func TestAutoDoesNotCache(t *testing.T) {
	s, r := planFixture()
	calls := 0
	p := counterPlanner(&calls)
	p.Plan(context.Background(), s, r)
	p.Plan(context.Background(), s, r)
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestDisabledExplicitRejected(t *testing.T) {
	s, r := planFixture()
	s.Models[0].Enabled = false
	r.Model = "a"
	calls := 0
	if _, e := counterPlanner(&calls).Plan(context.Background(), s, r); e == nil || calls != 0 {
		t.Fatal("disabled selected")
	}
}
func TestSnapshotStableDuringDecision(t *testing.T) {
	s, r := planFixture()
	entered, release := make(chan struct{}), make(chan struct{})
	p := Planner{Decider: chooseFunc(func(context.Context, DecisionConfig, DecisionInput) (Decision, error) {
		close(entered)
		<-release
		return Decision{ModelID: "a"}, nil
	})}
	done := make(chan Plan)
	go func() {
		v, e := p.Plan(context.Background(), s, r)
		if e != nil {
			t.Error(e)
		}
		done <- v
	}()
	<-entered
	s.Models[0].Enabled = false
	s.Providers[0].BaseURL = "changed"
	close(release)
	old := <-done
	if !old.Target.Model.Enabled || old.Target.Provider.BaseURL == "changed" {
		t.Fatal("snapshot changed")
	}
	r.Model = "a"
	if _, e := p.Plan(context.Background(), s, r); e == nil {
		t.Fatal("new snapshot ignored")
	}
}
func TestNoCandidateFitsContext(t *testing.T) {
	s, r := planFixture()
	calls := 0
	p := counterPlanner(&calls)
	p.Estimate = func(Model, Request) (int64, bool, error) { return 20000, false, nil }
	if plan, e := p.Plan(context.Background(), s, r); e != nil || calls != 1 || plan.Path != "context_estimate_fallback" || plan.ModelID != "a" {
		t.Fatalf("missing fallback: %+v %v", plan, e)
	}
}

func TestTextFormatDoesNotRequireStructuredOutput(t *testing.T) {
	s, r := planFixture()
	r.Model = "a"
	r.Options = map[string]json.RawMessage{"response_format": json.RawMessage(`{"type":"text"}`)}
	calls := 0
	if _, e := counterPlanner(&calls).Plan(context.Background(), s, r); e != nil {
		t.Fatal(e)
	}
}
func TestUnconfiguredIsDistinctFromFilteredCandidates(t *testing.T) {
	for _, mode := range []string{"empty", "disabled", "filtered"} {
		s, r := planFixture()
		switch mode {
		case "empty":
			s.Models = nil
		case "disabled":
			for i := range s.Models {
				s.Models[i].Enabled = false
			}
		case "filtered":
			r.Tools = []Tool{{Type: "function", Function: Function{Name: "f", Parameters: json.RawMessage(`{"type":"object"}`)}}}
			for i := range s.Models {
				s.Models[i].Capabilities.Tools = false
			}
		}
		calls := 0
		_, e := counterPlanner(&calls).Plan(context.Background(), s, r)
		want := "config_missing"
		if mode == "filtered" {
			want = "no_candidates"
		}
		known, ok := e.(*Error)
		if !ok || known.Code != want {
			t.Fatalf("%s got %v want %s", mode, e, want)
		}
	}
}

func TestToolHistoryFiltersCandidates(t *testing.T) {
	s, r := planFixture()
	if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"20 C"}]}`), &r); err != nil {
		t.Fatal(err)
	}
	calls := 0
	planner := counterPlanner(&calls)
	for _, id := range []string{"auto", "a"} {
		r.Model = id
		if _, err := planner.Plan(context.Background(), s, r); err == nil {
			t.Fatalf("%s accepted tool history without capability", id)
		}
	}
	s.Models[1].Capabilities.Tools = true
	r.Model = "auto"
	plan, err := planner.Plan(context.Background(), s, r)
	if err != nil || plan.ModelID != "b" || calls != 0 {
		t.Fatalf("plan=%+v calls=%d err=%v", plan, calls, err)
	}
}

func TestFallbackEligibilityAndStableSelection(t *testing.T) {
	for _, mode := range []string{"largest", "tie", "partial", "capability", "disabled", "check", "exact", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			s, r := planFixture()
			s.Models[0].Capabilities.ContextLimit = 100
			s.Models[1].Capabilities.ContextLimit = 200
			calls := 0
			p := counterPlanner(&calls)
			p.Estimate = func(Model, Request) (int64, bool, error) { return 500, false, nil }
			want, path := "b", "context_estimate_fallback"
			switch mode {
			case "tie":
				s.Models[0].Capabilities.ContextLimit = 200
				s.Models[0], s.Models[1] = s.Models[1], s.Models[0]
				want = "b"
			case "partial":
				s.Models[0].Capabilities.ContextLimit = 1000
				want = "a"
				path = "single_candidate"
			case "capability":
				r.Tools = []Tool{{Type: "function", Function: Function{Name: "f", Parameters: json.RawMessage(`{"type":"object"}`)}}}
				s.Models[0].Capabilities.Tools = true
				want = "a"
			case "disabled":
				s.Models[1].Enabled = false
				want = "a"
			case "check":
				p.Check = func(target Target, _ Request) error {
					if target.Model.ID == "b" {
						return Fail("unsupported_request", "blocked")
					}
					return nil
				}
				want = "a"
			case "exact":
				p.Estimate = func(Model, Request) (int64, bool, error) { return 500, true, nil }
			case "explicit":
				r.Model = "a"
				want = "a"
				path = "explicit"
				p.Estimate = func(Model, Request) (int64, bool, error) { t.Fatal("explicit invoked estimator"); return 0, false, nil }
			}
			plan, err := p.Plan(context.Background(), s, r)
			if mode == "exact" {
				if err == nil {
					t.Fatal("unexpected fallback")
				}
				return
			}
			wantCalls := 0
			if mode == "tie" {
				wantCalls = 1
			}
			if err != nil || plan.ModelID != want || plan.Path != path || calls != wantCalls || plan.ContextExact || (mode == "explicit" && plan.ContextEstimate != nil) {
				t.Fatalf("%+v err=%v calls=%d", plan, err, calls)
			}
		})
	}
}

func TestFallbackJevChoosesOnlyLargestTies(t *testing.T) {
	for _, mode := range []string{"selected", "unknown", "failure", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, r := planFixture()
			s.Models[0].Capabilities.ContextLimit = 100
			s.Models[1].Capabilities.ContextLimit = 200
			c := s.Models[1]
			c.ID = "c"
			s.Models = append(s.Models, c)
			calls := 0
			p := Planner{Decider: chooseFunc(func(_ context.Context, _ DecisionConfig, in DecisionInput) (Decision, error) {
				calls++
				if len(in.Candidates) != 2 || in.Candidates[0].ID != "b" || in.Candidates[1].ID != "c" {
					t.Fatalf("candidates: %+v", in.Candidates)
				}
				if string(in.Request.Messages[0].Content) != string(r.Messages[0].Content) {
					t.Fatal("request changed")
				}
				if mode == "failure" {
					return Decision{}, Fail("upstream_error", "failed")
				}
				if mode == "unknown" {
					return Decision{ModelID: "a"}, nil
				}
				return Decision{ModelID: "c", Usage: &Usage{InputTokens: 7}}, nil
			})}
			if mode == "missing" {
				p.Decider = nil
			}
			plan, err := p.Plan(context.Background(), s, r)
			if mode == "selected" {
				if err != nil || plan.ModelID != "c" || plan.Path != "context_estimate_fallback" || len(plan.CandidateIDs) != 2 || plan.DecisionUsage == nil || plan.DecisionUsage.InputTokens != 7 {
					t.Fatalf("%+v %v", plan, err)
				}
			} else if err == nil {
				t.Fatal("expected failure without arbitrary selection")
			}
			want := 1
			if mode == "missing" {
				want = 0
			}
			if calls != want {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
