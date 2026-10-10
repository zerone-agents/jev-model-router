package httptransport

import (
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func nativeChatHandler(t *testing.T) http.Handler {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "local-key" {
			t.Errorf("native URL/auth mismatch")
		}
		raw, _ := json.Marshal(b["messages"])
		history := strings.Contains(string(raw), `"tool_result"`)
		tools, _ := b["tools"].([]any)
		blocks := []map[string]any{{"type": "text", "text": "answer"}}
		stop := "end_turn"
		if len(tools) > 0 && !history {
			blocks = []map[string]any{{"type": "tool_use", "id": "c1", "name": "lookup", "input": map[string]any{"q": "value"}}}
			stop = "tool_use"
		}
		if history && !strings.Contains(string(raw), "tool-result-value") {
			t.Error("tool result lost")
		}
		if thinking, _ := b["thinking"].(map[string]any); thinking["type"] != "disabled" {
			if history && !strings.Contains(string(raw), `"thinking":"thought"`) {
				t.Error("thinking history lost")
			}
			blocks = append([]map[string]any{{"type": "thinking", "thinking": "thought"}}, blocks...)
		}
		if b["stream"] != true {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "upstream", "content": blocks, "stop_reason": stop, "usage": map[string]int{"input_tokens": 10, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 30, "output_tokens": 7}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(typ string, event map[string]any) {
			event["type"] = typ
			bytes, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, bytes)
		}
		send("message_start", map[string]any{"message": map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "upstream", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 30, "output_tokens": 0}}})
		for i, b := range blocks {
			start := map[string]any{"type": b["type"]}
			delta := map[string]any{}
			switch b["type"] {
			case "text":
				start["text"] = ""
				delta["type"] = "text_delta"
				delta["text"] = b["text"]
			case "thinking":
				start["thinking"] = ""
				delta["type"] = "thinking_delta"
				delta["thinking"] = b["thinking"]
			case "tool_use":
				start["id"] = b["id"]
				start["name"] = b["name"]
				start["input"] = map[string]any{}
				args, _ := json.Marshal(b["input"])
				delta["type"] = "input_json_delta"
				delta["partial_json"] = string(args)
			}
			send("content_block_start", map[string]any{"index": i, "content_block": start})
			send("content_block_delta", map[string]any{"index": i, "delta": delta})
			send("content_block_stop", map[string]any{"index": i})
		}
		send("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop}, "usage": map[string]int{"output_tokens": 7}})
		send("message_stop", map[string]any{})
	}))
	t.Cleanup(up.Close)
	g, e := provider.New(func(string) ([]byte, error) { return []byte("local-key"), nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { g.(io.Closer).Close() })
	cfg := snapshot()
	cfg.Providers[0].BaseURL = up.URL + "/v1"
	cfg.Providers[0].Protocol = routing.ProtocolAnthropic
	cfg.Models[0].UpstreamName = "claude-sonnet-4-5"
	cfg.Models[0].Capabilities.ContextLimit = 100000
	cfg.Models[0].Capabilities.Tools = true
	store := testStore{cfg}
	return NewHandler(management.New(store, nil), store, &routing.Planner{PrepareCheck: provider.PrepareCheck}, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
}
func TestChatAnthropicUpstreamHTTP(t *testing.T) {
	h := nativeChatHandler(t)
	for _, model := range []string{"auto", "external"} {
		for _, stream := range []bool{false, true} {
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t,"chat_template_kwargs":{"enable_thinking":true}}`, model, stream)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "thought") || !strings.Contains(w.Body.String(), `"model":"external"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if stream && !strings.Contains(w.Body.String(), "[DONE]") {
				t.Fatal("missing terminal")
			}
		}
	}
	w := messagesCall(h, `{"model":"external","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`, "key")
	if w.Code == 200 {
		t.Fatal("unverified native Messages route allowed")
	}
}
func TestNativeUpstreamSDKs(t *testing.T) {
	root := os.Getenv("JEV_TEST_OPENAI_SDK")
	agent := os.Getenv("JEV_TEST_AGENT_SDK")
	if root == "" || agent == "" {
		t.Skip("set JEV_TEST_OPENAI_SDK and JEV_TEST_AGENT_SDK")
	}
	server := httptest.NewServer(nativeChatHandler(t))
	defer server.Close()
	for _, name := range []string{"openai", "agent"} {
		args := []string{}
		if name == "agent" {
			args = append(args, "--require", filepath.Join(agent, "node_modules/tsx/dist/preflight.cjs"), "--import", filepath.Join(agent, "node_modules/tsx/dist/loader.mjs"))
		}
		args = append(args, "testdata/native-upstream-sdk.mjs", server.URL+"/v1", root, agent, name)
		cmd := exec.Command("node", args...)
		out, e := cmd.CombinedOutput()
		t.Log(string(out))
		if e != nil {
			t.Fatalf("%s SDK: %v", name, e)
		}
	}
}
