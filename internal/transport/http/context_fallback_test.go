package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestContextFallbackInspectAndFullGeneration(t *testing.T) {
	input := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "system", "content": "keep system"}, map[string]any{"role": "developer", "content": "keep developer"}, map[string]any{"role": "user", "content": strings.Repeat("full context ", 2000)}, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}}}}, map[string]any{"role": "tool", "tool_call_id": "call1", "content": "full tool output"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "description": "complete definition", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}}, "max_completion_tokens": 1024}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var got map[string]any
		json.NewDecoder(r.Body).Decode(&got)
		// The adapter adds stream indexes to historical calls and null content.
		// Neither changes call arguments, IDs, results or message order.
		messages, _ := got["messages"].([]any)
		for _, value := range messages {
			m := value.(map[string]any)
			if m["role"] == "assistant" {
				if m["content"] == nil {
					delete(m, "content")
				}
				if calls, ok := m["tool_calls"].([]any); ok {
					for _, call := range calls {
						delete(call.(map[string]any), "index")
					}
				}
			}
		}
		for _, key := range []string{"messages", "tools", "max_completion_tokens"} {
			if !reflect.DeepEqual(got[key], input[key]) { // Normalize numeric source value through JSON below.
				t.Errorf("generation changed %s", key)
			}
		}
		if got["model"] != "gpt-4o-mini" {
			t.Error("wrong model")
		}
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
		}
	}))
	defer upstream.Close()
	encoded, _ := json.Marshal(input)
	json.Unmarshal(encoded, &input)
	g, err := provider.New(func(string) ([]byte, error) { return []byte("test"), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer g.(io.Closer).Close()
	cfg := snapshot()
	cfg.Providers[0].BaseURL = upstream.URL
	cfg.Models[0].Capabilities.Tools = true
	cfg.Models[0].Capabilities.ContextLimit = 100
	cfg.Models[0].UpstreamName = "small"
	large := cfg.Models[0]
	large.ID = "largest"
	large.UpstreamName = "gpt-4o-mini"
	large.Capabilities.ContextLimit = 200
	cfg.Models = append(cfg.Models, large)
	var request routing.Request
	json.Unmarshal(encoded, &request)
	if err := provider.Check(routing.Target{Provider: cfg.Providers[0], Model: large}, request); err != nil {
		t.Fatal(err)
	}
	store := testStore{cfg}
	planner := &routing.Planner{Check: provider.Check}
	svc := management.New(store, nil)
	checks := management.Checks{Store: store, Planner: planner}
	checks.Register(svc)
	result, err := svc.Execute(context.Background(), "settings", management.Call{CapabilityID: "route.inspect", Input: encoded})
	if err != nil {
		t.Fatal(err)
	}
	var plan routing.Plan
	json.Unmarshal(result.Data, &plan)
	if plan.ModelID != "largest" || plan.Path != "context_estimate_fallback" || plan.ContextExact || plan.ContextEstimate == nil {
		t.Fatalf("%s", result.Data)
	}
	if calls.Load() != 0 {
		t.Fatal("inspect called generator")
	}
	handler := NewHandler(svc, store, planner, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	for _, stream := range []bool{false, true} {
		input["stream"] = stream
		body, _ := json.Marshal(input)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected retries: %d", calls.Load())
	}
}
