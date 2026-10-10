package provider

import (
	"bytes"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"testing"
)

func nativeMessagesTarget() routing.Target {
	return routing.Target{Provider: routing.Provider{Protocol: routing.ProtocolAnthropic}, Model: routing.Model{ID: "public", UpstreamName: "upstream"}}
}

func TestMessagesNativePreparation(t *testing.T) {
	raw := []byte(`{"model":"auto","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"signed"},{"type":"redacted_thinking","data":"opaque"},{"type":"tool_use","id":"t","name":"f","input":{"n":9007199254740993,"exp":1e100000}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"failed","is_error":true},{"type":"text","text":"continue","cache_control":{"type":"ephemeral"}}]}]}`)
	p, err := PrepareMessages(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = ' '
	}
	first, err := p.nativeBody(nativeMessagesTarget())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte(`9007199254740993`)) || !bytes.Contains(first, []byte(`1e100000`)) || !bytes.Contains(first, []byte(`"model":"upstream"`)) {
		t.Fatalf("lost raw data: %s", first)
	}
	first[0] = 'x'
	second, err := p.nativeBody(nativeMessagesTarget())
	if err != nil || !json.Valid(second) {
		t.Fatal("body shares storage", err)
	}
	if p.Summary() != "continue" {
		t.Fatal(p.Summary())
	}
}
func TestMessagesCandidateSpecificChecks(t *testing.T) {
	for _, extra := range []string{`,"thinking":{"type":"enabled","budget_tokens":1024}`, `,"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral"}}]`, `,"top_k":5`, `,"metadata":{"user_id":"secret"}`} {
		p, err := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"hello"}]` + extra + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Check(nativeMessagesTarget()); err != nil {
			t.Fatal(err)
		}
		if err = p.Check(routing.Target{}); err == nil {
			t.Fatal("OpenAI silently accepts native data")
		}
	}
}
func TestMessagesNativeValidation(t *testing.T) {
	fixtures := []string{
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"a","content":"b"}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"a","\u0063ontent":"b"}]}`,
		`{"model":"auto","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":"a"}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"orphan","content":"secret"}]}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":[{"type":"thinking","thinking":"secret","signature":"s"}]}]}`,
	}
	for _, raw := range fixtures {
		if _, err := PrepareMessages([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe error")
		}
	}
	cache := `{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}`
	for _, n := range []int{4, 5} {
		raw := `{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":[` + strings.TrimSuffix(strings.Repeat(cache+",", n), ",") + `]}]}`
		_, err := PrepareMessages([]byte(raw))
		if (err == nil) != (n == 4) {
			t.Fatalf("cache count %d: %v", n, err)
		}
	}
}

func TestMessagesNativeValidationBoundaries(t *testing.T) {
	for _, body := range []string{
		`{"model":"auto","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1023},"messages":[{"role":"user","content":"x"}]}`,
		`{"model":"auto","max_tokens":2048,"unknown":true,"messages":[{"role":"user","content":"x"}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":[{"type":"text","text":"a","cache_control":{"type":"ephemeral"}},{"type":"text","text":"b","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"s","cache_control":{"type":"ephemeral"}}]}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"x","cache_control":{"type":"ephemeral"}}]}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"f","input":{}},{"type":"tool_use","id":"t","name":"f","input":{}}]}]}`,
		`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"f","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"x"},{"type":"tool_result","tool_use_id":"t","content":"x"}]}]}`,
		"{\"model\":\"auto\",\"max_tokens\":2048,\"messages\":[{\"role\":\"user\",\"content\":\"\xff\"}]}",
	} {
		if _, err := PrepareMessages([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestMessagesNativeUnsignedProfileReplay(t *testing.T) {
	p, err := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"native unsigned"},{"type":"tool_use","id":"t","name":"f","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"ok"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	target := nativeMessagesTarget()
	target.Provider.BaseURL = "https://open.bigmodel.cn/api/anthropic/v1"
	target.Model.UpstreamName = "glm-5.3"
	if err = p.Check(target); err != nil {
		t.Fatal(err)
	}
	target.Provider.BaseURL = "https://api.anthropic.com/v1"
	if err = p.Check(target); err == nil {
		t.Fatal("unsigned replay accepted outside profile")
	}
}

func TestMessagesCandidateToolPrecision(t *testing.T) {
	for _, number := range []string{"9007199254740993", "1e100000"} {
		p, err := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"f","input":{"n":` + number + `}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"ok"}]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Check(nativeMessagesTarget()); err != nil {
			t.Fatal(err)
		}
		err = p.Check(target("https://example.invalid/v1"))
		if number == "9007199254740993" {
			r, e := p.chatRequest()
			if err != nil || e != nil || len(r.Messages) == 0 || r.Messages[0].ToolCalls[0].Function.Arguments != `{"n":9007199254740993}` {
				t.Fatalf("tool number changed: %v %v", err, e)
			}
		} else if err == nil || !strings.Contains(err.Error(), "unsupported_request") {
			t.Fatalf("unrepresentable Chat candidate: %s %v", number, err)
		}
	}
}
