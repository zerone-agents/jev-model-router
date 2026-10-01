package contracts

import (
	"encoding/json"
	"testing"
)

func TestReasoningContracts(t *testing.T) {
	for _, tc := range []struct {
		fields string
		valid  bool
	}{
		{`"reasoning_effort":"high"`, true},
		{`"chat_template_kwargs":{"enable_thinking":false}`, true},
		{`"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`, true},
		{`"reasoning_effort":"unknown"`, false},
		{`"reasoning_effort":null`, false},
		{`"chat_template_kwargs":{"enable_thinking":true,"model":"other"}`, false},
		{`"chat_template_kwargs":{"enable_thinking":"true"}`, false},
		{`"chat_template_kwargs":{}`, false},
	} {
		body := json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}],` + tc.fields + `}`)
		for _, id := range []string{"chat", "route.inspect"} {
			if valid := Validate(id, body) == nil; valid != tc.valid {
				t.Fatalf("%s %s: valid=%t", id, body, valid)
			}
		}
	}
	for _, role := range []string{"assistant", "user", "tool", "system"} {
		body := json.RawMessage(`{"model":"auto","messages":[{"role":"` + role + `","content":"hi","reasoning_content":"thought"}]}`)
		for _, id := range []string{"chat", "route.inspect"} {
			if valid := Validate(id, body) == nil; valid != (role == "assistant") {
				t.Fatalf("%s allowed reasoning on %s", id, role)
			}
		}
	}
}

func TestReasoningCapabilityCombinations(t *testing.T) {
	for _, tc := range []struct {
		combinations string
		valid        bool
	}{
		{`[]`, true},
		{`[{"enable_thinking":true},{"enable_thinking":false},{"reasoning_effort":"high"},{"enable_thinking":true,"reasoning_effort":"high"}]`, true},
		{`[{}]`, false},
		{`[{"enable_thinking":null}]`, false},
		{`[{"reasoning_effort":"unknown"}]`, false},
		{`[{"reasoning_effort":"high"},{"reasoning_effort":"high"}]`, false},
		{`[{"reasoning_effort":"high","provider":"override"}]`, false},
	} {
		b := json.RawMessage(`{"id":"m","provider_id":"p","upstream_name":"upstream","description":"","location":"cloud","enabled":true,"capabilities":{"context_limit":8192,"tools":false,"images":false,"structured_output":false,"reasoning":` + tc.combinations + `}}`)
		if valid := Validate("models.put", b) == nil; valid != tc.valid {
			t.Fatalf("%s: valid=%t", tc.combinations, valid)
		}
	}
}
