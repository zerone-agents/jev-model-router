package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeMessagesOfficialSDK(t *testing.T) {
	root := os.Getenv("JEV_TEST_ANTHROPIC_SDK")
	if root == "" {
		t.Skip("set JEV_TEST_ANTHROPIC_SDK")
	}
	var calls atomic.Int32
	initial := []any{map[string]any{"type": "thinking", "thinking": "thought", "signature": "signature-original"}, map[string]any{"type": "redacted_thinking", "data": "opaque-original"}, map[string]any{"type": "tool_use", "id": "c", "name": "lookup", "input": map[string]any{"q": "value"}}}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Error("invalid body")
		}
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "upstream-key" || r.Header.Get("anthropic-beta") != "" {
			t.Error("wire contract")
		}
		messages, _ := b["messages"].([]any)
		raw, _ := json.Marshal(messages)
		history := strings.Contains(string(raw), `"tool_result"`)
		truncated := strings.Contains(string(raw), `truncate`)
		missing := strings.Contains(string(raw), `missing_usage`)
		blocks := initial
		stop := "tool_use"
		if history {
			for _, v := range messages {
				m := v.(map[string]any)
				if m["role"] == "assistant" && !reflect.DeepEqual(m["content"], initial) {
					t.Errorf("assistant replay changed: %v", m)
				}
			}
			if !strings.Contains(string(raw), `"is_error":true`) {
				t.Error("lost error marker")
			}
			blocks = []any{map[string]any{"type": "text", "text": "answer"}}
			stop = "end_turn"
		}
		if truncated {
			blocks = []any{map[string]any{"type": "tool_use", "id": "c", "name": "lookup", "input": map[string]any{"partial": true}}}
			stop = "max_tokens"
		}
		if missing {
			blocks = []any{map[string]any{"type": "text", "text": "answer"}}
			stop = "end_turn"
		}
		usage := map[string]any{"input_tokens": float64(0), "output_tokens": float64(3), "cache_read_input_tokens": float64(20)}
		if b["stream"] != true {
			response := map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "up", "content": blocks, "stop_reason": stop, "stop_sequence": nil}
			if !missing {
				response["usage"] = usage
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(typ string, event map[string]any) {
			event["type"] = typ
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, raw)
		}
		start := map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "up", "content": []any{}, "stop_reason": nil, "stop_sequence": nil}
		if !missing {
			start["usage"] = usage
		}
		send("message_start", map[string]any{"message": start})
		for i, item := range blocks {
			block := item.(map[string]any)
			typ := block["type"].(string)
			head := map[string]any{"type": typ}
			var delta map[string]any
			switch typ {
			case "text":
				head["text"] = ""
				delta = map[string]any{"type": "text_delta", "text": block["text"]}
			case "thinking":
				head["thinking"] = ""
				head["signature"] = ""
				delta = map[string]any{"type": "thinking_delta", "thinking": block["thinking"]}
			case "redacted_thinking":
				head["data"] = block["data"]
			case "tool_use":
				head["id"] = block["id"]
				head["name"] = block["name"]
				head["input"] = map[string]any{}
				args, _ := json.Marshal(block["input"])
				value := string(args)
				if truncated {
					value = `{"q":`
				}
				delta = map[string]any{"type": "input_json_delta", "partial_json": value}
			}
			send("content_block_start", map[string]any{"index": i, "content_block": head})
			if delta != nil {
				send("content_block_delta", map[string]any{"index": i, "delta": delta})
			}
			if typ == "thinking" {
				send("content_block_delta", map[string]any{"index": i, "delta": map[string]any{"type": "signature_delta", "signature": block["signature"]}})
			}
			send("content_block_stop", map[string]any{"index": i})
		}
		end := map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}}
		if !missing {
			end["usage"] = map[string]any{"output_tokens": 3}
		}
		send("message_delta", end)
		send("message_stop", map[string]any{})
	}))
	defer up.Close()
	router := httptest.NewServer(nativeMessagesHandler(t, up.URL, nil, false, Limits{}))
	defer router.Close()
	cmd := exec.Command("node", "testdata/native-messages-sdk.mjs", router.URL, root)
	out, err := cmd.CombinedOutput()
	t.Log(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 14 {
		t.Fatalf("calls %d want14", calls.Load())
	}
}
