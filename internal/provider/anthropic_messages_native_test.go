package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeMessagesJSONWire(t *testing.T) {
	request := `{"model":"external","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`
	response := `{"id":"m","type":"message","role":"assistant","model":"upstream","content":[{"type":"thinking","thinking":"think","signature":"signed"},{"type":"redacted_thinking","data":"opaque"},{"type":"tool_use","id":"t","name":"f","input":{"n":9007199254740993,"e":1e100000}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":2,"cache_read_input_tokens":10,"cache_creation":{"ephemeral_1h_input_tokens":3},"future":{"n":9007199254740993}}}`
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("anthropic-beta") != "" {
			t.Error("unsafe headers/path")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"thinking":{"type":"enabled","budget_tokens":1024}`) {
			t.Errorf("lost body %s", body)
		}
		fmt.Fprint(w, response)
	}))
	defer s.Close()
	p, err := PrepareMessages([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	target := nativeTarget(s.URL + "/v1")
	target.Model.ID = "external"
	g := generator(t).(*bifrostGenerator)
	out, err := g.CompleteMessages(context.Background(), target, p)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(string(out), `9007199254740993`) || !strings.Contains(string(out), `1e100000`) || !strings.Contains(string(out), `"model":"external"`) {
		t.Fatalf("%d %s", calls, out)
	}
	var got, want map[string]json.RawMessage
	_ = json.Unmarshal(out, &got)
	_ = json.Unmarshal([]byte(response), &want)
	delete(got, "model")
	delete(want, "model")
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("response changed %s", out)
	}
}
func TestNativeMessagesJSONTerminal(t *testing.T) {
	p, _ := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"stop_sequences":["END"],"messages":[{"role":"user","content":"hi"}]}`))
	target := nativeMessagesTarget()
	for _, tc := range []struct {
		stop, content, sequence, usage string
		ok                             bool
	}{
		{"end_turn", `[{"type":"text","text":"ok"}]`, `null`, ``, true},
		{"max_tokens", `[{"type":"tool_use","id":"t","name":"f","input":{"partial":1}}]`, `null`, `,"usage":{"input_tokens":0}`, true},
		{"stop_sequence", `[{"type":"text","text":"ok"}]`, `"END"`, ``, true},
		{"stop_sequence", `[{"type":"text","text":"ok"}]`, `"OTHER"`, ``, false},
		{"refusal", `[]`, `null`, ``, true},
		{"tool_use", `[]`, `null`, ``, false},
		{"tool_use", `[{"type":"tool_use","id":"t","name":"f","input":[]}]`, `null`, ``, false},
		{"tool_use", `[{"type":"tool_use","id":"t","name":"f","input":{}},{"type":"tool_use","id":"t","name":"g","input":{}}]`, `null`, ``, false},
		{"pause_turn", `[{"type":"text","text":"ok"}]`, `null`, ``, false},
		{"end_turn", `[{"type":"text","text":"ok"}]`, `null`, `,"usage":{"output_tokens":-1}`, false},
		{"end_turn", `[{"type":"text","text":"ok"}]`, `null`, `,"usage":{"output_tokens":1.5}`, false},
	} {
		raw := fmt.Sprintf(`{"id":"m","type":"message","role":"assistant","model":"up","content":%s,"stop_reason":%q,"stop_sequence":%s%s}`, tc.content, tc.stop, tc.sequence, tc.usage)
		out, err := validateNativeMessagesResponse([]byte(raw), target, p)
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", raw, err)
		}
		if tc.ok && tc.usage == "" && strings.Contains(string(out), "usage") {
			t.Fatal("invented usage")
		}
	}
}
func TestNativeMessagesSignatureBoundary(t *testing.T) {
	p, _ := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`))
	raw := `{"id":"m","type":"message","role":"assistant","model":"up","content":[{"type":"thinking","thinking":"thought"},{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`
	for _, tc := range []struct {
		url, model string
		ok         bool
	}{{"https://open.bigmodel.cn/api/anthropic/v1", "glm-5.3", true}, {"https://open.bigmodel.cn/api/anthropic/v1", "glm-5.3-flash", true}, {"https://open.bigmodel.cn.evil/api/anthropic/v1", "glm-5.3", false}, {"https://open.bigmodel.cn/api/anthropic/v1", "glm-5.3-extra", false}, {"https://api.anthropic.com/v1", "claude-sonnet-4-5", false}} {
		target := nativeTarget(tc.url)
		target.Provider.BaseURL = tc.url
		target.Model.UpstreamName = tc.model
		_, err := validateNativeMessagesResponse([]byte(raw), target, p)
		if (err == nil) != tc.ok {
			t.Fatalf("%+v %v", tc, err)
		}
	}
}
