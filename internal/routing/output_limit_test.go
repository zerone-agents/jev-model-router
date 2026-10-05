package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/contracts"
)

func TestOutputLimitAliases(t *testing.T) {
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		for _, limit := range []int{16, 1024, 10000000} {
			body := fmt.Sprintf(`{"model":"auto","messages":[{"role":"system","content":"Reply briefly."},{"role":"user","content":"请用一句中文解释模型路由。"}],%q:%d}`, field, limit)
			var r Request
			if err := json.Unmarshal([]byte(body), &r); err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequest(r); err != nil {
				t.Fatal(err)
			}
			if err := contracts.Validate("chat", []byte(body)); err != nil {
				t.Fatal(err)
			}
			if string(r.Options["max_completion_tokens"]) != fmt.Sprint(limit) || r.Options["max_tokens"] != nil {
				t.Fatalf("not normalized: %v", r.Options)
			}
			for _, model := range []string{"auto", "fast"} {
				r.Model = model
				s := fixture()
				s.Models[0].Capabilities.ContextLimit = int64(limit) + 1000
				p := Planner{}
				if _, err := p.Plan(context.Background(), s, r); err != nil {
					t.Fatalf("%s: %v", model, err)
				}
				s.Models[0].Capabilities.ContextLimit = int64(limit)
				plan, err := p.Plan(context.Background(), s, r)
				if model == "auto" {
					if err != nil || plan.Path != "context_estimate_fallback" {
						t.Fatalf("missing fallback: %+v %v", plan, err)
					}
				} else if err != nil || plan.Path != "explicit" || plan.ContextEstimate != nil {
					t.Fatalf("explicit budget not delegated: %+v %v", plan, err)
				}
			}
		}
	}
}

func TestOutputLimitErrors(t *testing.T) {
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		for _, value := range []string{"null", "0", "15", "10000001", "-1", "1.5", "16.0000000000000001", `"1024"`, "true", "[]", "{}", "999999999999999999999999999999999"} {
			body := fmt.Sprintf(`{"model":"auto","messages":[{"role":"user","content":"SECRET"}],%q:%s}`, field, value)
			var r Request
			if err := json.Unmarshal([]byte(body), &r); err != nil {
				t.Fatal(err)
			}
			err := ValidateRequest(r)
			if err == nil || err.(*Error).Code != "invalid_request" || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("%s: %v", body, err)
			}
			if contracts.Validate("chat", []byte(body)) == nil {
				t.Fatalf("schema accepted %s", body)
			}
		}
	}
	for _, value := range []string{"1024", "null", "8"} {
		body := []byte(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"max_tokens":1024,"max_completion_tokens":` + value + `}`)
		var r Request
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatal(err)
		}
		if err := ValidateRequest(r); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatal(err)
		}
		if contracts.Validate("chat", body) == nil {
			t.Fatal("schema accepted conflict")
		}
	}
}

func TestChatErrorIdentifiesFieldWithoutValue(t *testing.T) {
	for _, field := range []string{"reasoning_effort", "chat_template_kwargs", "fallbacks", "temperature"} {
		body := fmt.Sprintf(`{"model":"auto","messages":[{"role":"user","content":"SECRET"}],%q:"SECRET"}`, field)
		var r Request
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		err := ValidateRequest(r)
		if err == nil || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
}

func TestNormalizedLimitReachesDecision(t *testing.T) {
	s, _ := planFixture()
	var r Request
	if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"max_tokens":1024}`), &r); err != nil {
		t.Fatal(err)
	}
	called := false
	p := Planner{Decider: chooseFunc(func(_ context.Context, _ DecisionConfig, in DecisionInput) (Decision, error) {
		called = true
		if string(in.Request.Options["max_completion_tokens"]) != "1024" || in.Request.Options["max_tokens"] != nil {
			t.Fatal("decision lost normalized limit")
		}
		return Decision{ModelID: in.Candidates[0].ID}, nil
	})}
	plan, err := p.Plan(context.Background(), s, r)
	if err != nil || !called || plan.Path != "jev_choice" {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestOutputLimitIntegerRepresentations(t *testing.T) {
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		for _, value := range []string{"1024.0", "1.024e3"} {
			var r Request
			body := fmt.Sprintf(`{"model":"auto","messages":[{"role":"user","content":"hi"}],%q:%s}`, field, value)
			if err := json.Unmarshal([]byte(body), &r); err != nil {
				t.Fatal(err)
			}
			if err := ValidateRequest(r); err != nil {
				t.Fatal(err)
			}
			if string(r.Options["max_completion_tokens"]) != "1024" {
				t.Fatal("integer value changed")
			}
		}
	}
}
