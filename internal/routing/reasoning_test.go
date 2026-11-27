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
				if model == "auto" && fields != `"chat_template_kwargs":{"enable_thinking":true}` {
					if err == nil {
						t.Fatal("Auto accepted manual controls")
					}
					continue
				}
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

func TestThinkingAliasValidation(t *testing.T) {
	for _, tc := range []struct{ fields, code string }{
		{`"enable_thinking":true`, ""},
		{`"enable_thinking":false,"chat_template_kwargs":{"enable_thinking":false}`, ""},
		{`"enable_thinking":true,"chat_template_kwargs":{"enable_thinking":false}`, "invalid_request"},
		{`"enable_thinking":null`, "unsupported_request"},
	} {
		err := ValidateChatSchema(json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}],` + tc.fields + "}"))
		if tc.code == "" && err != nil || tc.code != "" && (err == nil || err.(*Error).Code != tc.code) {
			t.Errorf("%s: got %v, want %q", tc.fields, err, tc.code)
		}
	}
}
