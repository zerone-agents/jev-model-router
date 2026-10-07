package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUnmodifiedAnthropicSDK(t *testing.T) {
	root := os.Getenv("JEV_TEST_ANTHROPIC_SDK")
	if root == "" {
		t.Skip("set JEV_TEST_ANTHROPIC_SDK to a Node project with @anthropic-ai/sdk installed")
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Stream   bool
			Messages []struct {
				Role      string
				Content   json.RawMessage
				Reasoning string `json:"reasoning_content"`
			}
			Parallel *bool  `json:"parallel_tool_calls"`
			Effort   string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, m := range body.Messages {
			if string(m.Content) == `"force_stream_failure"` {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
				return
			}
		}
		if body.Parallel == nil || *body.Parallel || body.Effort != "high" {
			t.Errorf("parameters: %+v", body)
		}
		history := false
		for _, m := range body.Messages {
			if m.Role == "tool" {
				history = true
				if string(m.Content) != `"result"` {
					t.Error("tool result changed")
				}
			}
			if m.Role == "assistant" && m.Reasoning != "thought" {
				t.Errorf("thinking history missing: %q", m.Reasoning)
			}
		}
		if !body.Stream {
			message := map[string]any{"role": "assistant", "content": "answer", "reasoning_content": "thought"}
			finish := "stop"
			if !history {
				message["content"] = nil
				message["tool_calls"] = []any{map[string]any{"id": "call1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"q":"value"}`}}}
				finish = "tool_calls"
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "x", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\n")
		if history {
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"value\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		}
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	router := httptest.NewServer(messagesHandler(t, upstream.URL, Limits{}))
	defer router.Close()
	cmd := exec.Command("node", "testdata/anthropic-sdk.mjs", router.URL, root)
	out, err := cmd.CombinedOutput()
	t.Log(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 9 {
		t.Fatalf("upstream calls=%d want9", calls.Load())
	}
}
