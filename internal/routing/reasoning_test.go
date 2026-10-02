package routing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningNeedsNoCapabilityDeclaration(t *testing.T) {
	for _, declared := range [][]ReasoningCombination{nil, {}, {{EnableThinking: new(false), Effort: "low"}}} {
		s, _ := planFixture()
		s.Models = s.Models[:1]
		s.Models[0].Capabilities.Reasoning = declared
		for _, model := range []string{"auto", "a"} {
			for _, fields := range []string{`"chat_template_kwargs":{"enable_thinking":true}`, `"chat_template_kwargs":{"enable_thinking":false}`, `"reasoning_effort":"high"`, `"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`} {
				var r Request
				if err := json.Unmarshal([]byte(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}],`+fields+`}`), &r); err != nil {
					t.Fatal(err)
				}
				p := Planner{}
				plan, err := p.Plan(context.Background(), s, r)
				if err != nil || plan.ModelID != "a" {
					t.Fatalf("%s %s: %+v %v", model, fields, plan, err)
				}
			}
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
