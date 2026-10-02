package routing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningEligibility(t *testing.T) {
	enabled, disabled := true, false
	s, _ := planFixture()
	s.Models[0].Capabilities.Reasoning = []ReasoningCombination{{EnableThinking: &enabled}, {Effort: "high"}}
	s.Models[1].Capabilities.Reasoning = []ReasoningCombination{{EnableThinking: &disabled}}
	for _, tc := range []struct {
		fields, model, want string
		fail                bool
	}{
		{`"chat_template_kwargs":{"enable_thinking":true}`, "auto", "a", false},
		{`"reasoning_effort":"high"`, "auto", "a", false},
		{`"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`, "auto", "a", false},
		{`"chat_template_kwargs":{"enable_thinking":false}`, "auto", "b", false},
		{`"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`, "a", "a", false},
		{`"reasoning_effort":"low"`, "auto", "", true},
		{`"reasoning_effort":"low"`, "a", "", true},
	} {
		var r Request
		if err := json.Unmarshal([]byte(`{"model":"`+tc.model+`","messages":[{"role":"user","content":"hi"}],`+tc.fields+`}`), &r); err != nil {
			t.Fatal(err)
		}
		p := Planner{}
		plan, err := p.Plan(context.Background(), s, r)
		if (err != nil) != tc.fail || (!tc.fail && plan.ModelID != tc.want) {
			t.Fatalf("%s %s: %+v %v", tc.model, tc.fields, plan, err)
		}
	}
}

func TestInvalidReasoningControls(t *testing.T) {
	for _, fields := range []string{
		`"reasoning_effort":"SECRET"`, `"reasoning_effort":null`, `"reasoning_effort":true`,
		`"chat_template_kwargs":null`, `"chat_template_kwargs":{}`, `"chat_template_kwargs":{"enable_thinking":null}`,
		`"chat_template_kwargs":{"enable_thinking":"SECRET"}`, `"chat_template_kwargs":{"enable_thinking":true,"provider":"SECRET"}`,
	} {
		var r Request
		if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"user","content":"SECRET"}],`+fields+`}`), &r); err != nil {
			t.Fatal(err)
		}
		if err := ValidateRequest(r); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("%s: %v", fields, err)
		}
	}
}

func TestCombinedDeclarationAllowsIndependentControls(t *testing.T) {
	enabled := true
	c := Capabilities{Reasoning: []ReasoningCombination{{EnableThinking: &enabled, Effort: "high"}}}
	for _, fields := range []string{`"chat_template_kwargs":{"enable_thinking":true}`, `"reasoning_effort":"high"`, `"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`} {
		var r Request
		if err := json.Unmarshal([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}],`+fields+`}`), &r); err != nil {
			t.Fatal(err)
		}
		if err := CheckReasoning(c, r); err != nil {
			t.Fatal(err)
		}
	}
}
