package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// Run the original SDK example, including its default tool pool and real Read
// execution. Only the upstream model is scripted, not the SDK or router.
func TestAgentSDKSimpleQuerySample(t *testing.T) {
	sdk := os.Getenv("JEV_TEST_AGENT_SDK")
	if sdk == "" {
		t.Skip("set JEV_TEST_AGENT_SDK to an agent-sdk checkout with tsx installed")
	}
	for _, tc := range []struct{ model, sample string }{
		{"auto", "basic/01-simple-query.ts"}, {"external", "basic/01-simple-query.ts"},
		{"auto", "streaming/16-streaming.ts"}, {"external", "streaming/16-streaming.ts"},
	} {
		model := tc.model
		t.Run(model+"/"+tc.sample, func(t *testing.T) {
			var calls atomic.Int32
			originalTools := make(chan any, 64)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Stream bool `json:"stream"`
					Tools  []struct {
						Function struct{ Name, Description string }
					}
					Messages []struct {
						Role    string
						Content json.RawMessage
					}
				}
				raw, _ := io.ReadAll(r.Body)
				var wire map[string]any
				json.Unmarshal(raw, &wire)
				if !reflect.DeepEqual(wire["tools"], <-originalTools) {
					t.Error("tool definitions changed in transit")
				}
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
				calls.Add(1)
				long := false
				for _, tool := range body.Tools {
					if len(tool.Function.Description) > 4096 {
						long = true
					}
				}
				if !long {
					t.Error("SDK long tool descriptions missing upstream")
				}
				history := false
				for _, m := range body.Messages {
					if m.Role == "tool" && strings.Contains(string(m.Content), "router-sample-fixture") {
						history = true
					}
				}
				if !body.Stream {
					w.Header().Set("Content-Type", "application/json")
					message := map[string]any{"role": "assistant", "content": "router-sample-fixture version 1.0.0"}
					finish := "stop"
					if !history {
						message["content"] = nil
						message["tool_calls"] = []any{map[string]any{"id": "read1", "type": "function", "function": map[string]any{"name": "Read", "arguments": `{"file_path":"package.json"}`}}}
						finish = "tool_calls"
					}
					json.NewEncoder(w).Encode(map[string]any{"id": "sample", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if !history {
					fmt.Fprint(w, "data: {\"id\":\"sample\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"read1\",\"type\":\"function\",\"function\":{\"name\":\"Read\",\"arguments\":\"{\\\"file_path\\\":\\\"package.json\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"id\":\"sample\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"router-sample-fixture version 1.0.0\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer upstream.Close()
			g, err := provider.New(func(string) ([]byte, error) { return []byte("local"), nil })
			if err != nil {
				t.Fatal(err)
			}
			defer g.(io.Closer).Close()
			cfg := snapshot()
			cfg.Providers[0].BaseURL = upstream.URL
			cfg.Models[0].UpstreamName = "gpt-4o-mini"
			cfg.Models[0].Capabilities.Tools = true
			cfg.Models[0].Capabilities.Reasoning = []routing.ReasoningCombination{{EnableThinking: new(true)}}
			cfg.Models[0].Capabilities.ContextLimit = 1000000
			store := testStore{cfg}
			h := NewHandler(management.New(store, nil), store, &routing.Planner{Check: provider.Check}, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(raw))
				var input map[string]any
				json.Unmarshal(raw, &input)
				originalTools <- input["tools"]
				h.ServeHTTP(w, r)
			}))
			defer server.Close()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"router-sample-fixture","version":"1.0.0"}`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "--require", filepath.Join(sdk, "node_modules/tsx/dist/preflight.cjs"), "--import", filepath.Join(sdk, "node_modules/tsx/dist/loader.mjs"), filepath.Join(sdk, "examples", tc.sample))
			cmd.WaitDelay = 5 * time.Second
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "ZERONE_AGENT_API_TYPE=openai-completions", "ZERONE_AGENT_API_KEY=local-test", "ZERONE_AGENT_MODEL="+model, "ZERONE_AGENT_BASE_URL="+server.URL+"/v1")
			out, err := cmd.CombinedOutput()
			if err != nil || (!strings.Contains(string(out), "subtype: success") && !strings.Contains(string(out), "Result: success")) || !strings.Contains(string(out), "router-sample-fixture version 1.0.0") || calls.Load() != 2 {
				t.Fatalf("sample failed (%v), calls=%d: %s", err, calls.Load(), out)
			}
		})
	}
}
