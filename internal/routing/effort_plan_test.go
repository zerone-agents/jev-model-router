package routing

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEffortPlanning(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count        int
		allowed      []string
		target, want string
		calls        int
	}{
		{"multiple", 2, []string{"low", "high"}, "xhigh", "high", 1},
		{"minimum", 2, []string{"medium", "high"}, "minimal", "medium", 1},
		{"sole", 1, []string{"low", "high"}, "medium", "low", 1},
		{"singleton", 1, []string{"high"}, "", "high", 0},
		{"legacy", 1, nil, "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := planFixture()
			s.Models = s.Models[:tc.count]
			if tc.allowed != nil {
				s.Models[0].AutoEffort = &AutoEffortConfig{Allowed: tc.allowed}
			}
			calls := 0
			p := Planner{Decider: chooseFunc(func(_ context.Context, _ DecisionConfig, in DecisionInput) (Decision, error) {
				calls++
				if !in.SelectEffort {
					t.Error("missing effort question")
				}
				return Decision{ModelID: "a", Effort: tc.target}, nil
			})}
			plan, err := p.Plan(context.Background(), s, r)
			if err != nil || plan.EffectiveEffort != tc.want || calls != tc.calls {
				t.Fatalf("%+v %v calls=%d", plan, err, calls)
			}
			if len(r.Options) != 0 {
				t.Fatal("mutated caller")
			}
		})
	}
}
func TestEffortCallerPrecedence(t *testing.T) {
	for _, options := range []map[string]json.RawMessage{{"reasoning_effort": json.RawMessage(`"max"`)}, {"chat_template_kwargs": json.RawMessage(`{"enable_thinking":false}`)}} {
		s, r := planFixture()
		s.Models = s.Models[:1]
		s.Models[0].AutoEffort = &AutoEffortConfig{Allowed: []string{"low", "high"}}
		r.Options = options
		calls := 0
		plan, err := counterPlanner(&calls).Plan(context.Background(), s, r)
		if err != nil || calls != 0 {
			t.Fatalf("%+v %v calls=%d", plan, err, calls)
		}
	}
}

func TestExplicitNoneInspectionAfterProviderAdaptation(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		s, r := planFixture()
		s.Providers[0].Protocol = ProviderProtocol(protocol)
		r.Model = "a"
		r.Options = map[string]json.RawMessage{"reasoning_effort": json.RawMessage(`"none"`)}
		p, err := (&Planner{}).Plan(context.Background(), s, r)
		if err != nil {
			t.Fatal(err)
		}
		want := "none"
		if protocol == "anthropic" {
			want = ""
		}
		if p.EffectiveEffort != want {
			t.Fatalf("%s inspection effort=%q want %q", protocol, p.EffectiveEffort, want)
		}
		if ExplicitEffort(WithEffectiveEffort(r, p.EffectiveEffort)) != "none" {
			t.Fatal("explicit caller control lost")
		}
	}
}
