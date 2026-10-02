package httptransport

import (
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

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type reasoningDecider struct{ t *testing.T }

func (d reasoningDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	ids := []string{}
	for _, m := range in.Candidates {
		ids = append(ids, m.ID)
	}
	if in.Request.Options["reasoning_effort"] != nil || in.Request.Options["chat_template_kwargs"] != nil {
		if !reflect.DeepEqual(ids, []string{"external", "other", "plain"}) {
			d.t.Errorf("ineligible candidates: %v", ids)
		}
	}
	return routing.Decision{ModelID: "external"}, nil
}

func reasoningRouter(t *testing.T) (http.Handler, <-chan map[string]json.RawMessage, *atomic.Int32) {
	t.Helper()
	captured := make(chan map[string]json.RawMessage, 64)
	calls := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		captured <- body
		if string(body["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			// First event contains only thinking: it must commit headers and reset idle time.
			fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"thought"},"finish_reason":"stop"}]}`)
		}
	}))
	t.Cleanup(upstream.Close)
	g, err := provider.New(func(string) ([]byte, error) { return []byte("test"), nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.(io.Closer).Close() })
	cfg := snapshot()
	cfg.Providers[0].BaseURL = upstream.URL
	cfg.Models[0].UpstreamName = "qwen-test"
	cfg.Models[0].Capabilities.Reasoning = nil
	other := cfg.Models[0]
	other.ID = "other"
	other.Capabilities.Reasoning = []routing.ReasoningCombination{{EnableThinking: new(false), Effort: "low"}}
	unsupported := cfg.Models[0]
	unsupported.ID = "plain"
	unsupported.Capabilities.Reasoning = nil
	cfg.Models = append(cfg.Models, other, unsupported)
	cfg.Decision.Model = "jev-test"
	store := testStore{cfg}
	planner := &routing.Planner{Check: provider.Check, Decider: reasoningDecider{t}}
	svc := management.New(store, nil)
	(&management.Checks{Store: store, Planner: planner, Generator: g}).Register(svc)
	return NewHandler(svc, store, planner, &routing.Executor{Generator: g}, func(token string) (management.Principal, error) {
		role := "inference"
		if token == "Bearer settings" {
			role = "settings"
		}
		return management.Principal{Role: role}, nil
	}, Limits{}), captured, calls
}

func TestReasoningThroughRouterAndInspection(t *testing.T) {
	h, captured, calls := reasoningRouter(t)
	for _, model := range []string{"auto", "external"} {
		for _, fields := range []string{`"chat_template_kwargs":{"enable_thinking":true}`, `"reasoning_effort":"high"`, `"chat_template_kwargs":{"enable_thinking":true},"reasoning_effort":"high"`, `"chat_template_kwargs":{"enable_thinking":false}`, `"reasoning_effort":"low"`, `"chat_template_kwargs":{"enable_thinking":false},"reasoning_effort":"low"`} {
			for _, stream := range []bool{false, true} {
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1024,"stream":%t,%s}`, model, stream, fields)
				before := calls.Load()
				inspect := httptest.NewRecorder()
				q := httptest.NewRequest("POST", "/admin/v1/call/route.inspect", strings.NewReader(`{"input":`+body+`}`))
				q.Header.Set("Authorization", "Bearer settings")
				h.ServeHTTP(inspect, q)
				if inspect.Code != 200 || !strings.Contains(inspect.Body.String(), `"model_id":"external"`) || calls.Load() != before {
					t.Fatalf("inspect: %d %s", inspect.Code, inspect.Body.String())
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
				result := w.Body.String()
				if w.Code != 200 || !strings.Contains(result, `"reasoning_content":"thought"`) || !strings.Contains(result, `"content":"ok"`) || !strings.Contains(result, `"finish_reason":"stop"`) || (stream && !strings.Contains(result, "[DONE]")) {
					t.Fatalf("generation: %d %s", w.Code, result)
				}
				if calls.Load() != before+1 {
					t.Fatal("generation retried")
				}
				wire := <-captured
				var original map[string]json.RawMessage
				_ = json.Unmarshal([]byte(body), &original)
				for _, key := range []string{"chat_template_kwargs", "reasoning_effort", "max_completion_tokens"} {
					var got, want any
					_ = json.Unmarshal(wire[key], &got)
					_ = json.Unmarshal(original[key], &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: got %v want %v", key, got, want)
					}
				}
			}
		}
	}
	for _, tc := range []struct {
		model, fields string
		status        int
	}{
		{"auto", `"reasoning_effort":"SECRET"`, 400},
		{"auto", `"chat_template_kwargs":{"enable_thinking":true,"override":"SECRET"}`, 400},
	} {
		for _, stream := range []bool{false, true} {
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"SECRET"}],"stream":%t,%s}`, tc.model, stream, tc.fields)
			before := calls.Load()
			for _, inspect := range []bool{false, true} {
				path := "/v1/chat/completions"
				payload := body
				if inspect {
					path = "/admin/v1/call/route.inspect"
					payload = `{"input":` + body + `}`
				}
				q := httptest.NewRequest("POST", path, strings.NewReader(payload))
				if inspect {
					q.Header.Set("Authorization", "Bearer settings")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, q)
				if w.Code != tc.status || strings.Contains(w.Body.String(), "SECRET") {
					t.Fatalf("rejection: %d %s", w.Code, w.Body.String())
				}
			}
			if calls.Load() != before {
				t.Fatal("rejected input reached upstream")
			}
		}
	}
}

func TestUnmodifiedAgentSDKReasoning(t *testing.T) {
	sdk := os.Getenv("JEV_TEST_AGENT_SDK")
	if sdk == "" {
		t.Skip("set JEV_TEST_AGENT_SDK to an agent-sdk checkout with tsx installed")
	}
	h, captured, calls := reasoningRouter(t)
	server := httptest.NewServer(h)
	defer server.Close()
	script, err := filepath.Abs("../../../scripts/verify-agent-sdk.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(sdk, "node_modules/.bin/tsx"), script, sdk, server.URL+"/v1", "reasoning")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SDK verification: %v\n%s", err, out)
	}
	t.Log(string(out))
	if calls.Load() != 12 {
		t.Fatalf("SDK calls=%d", calls.Load())
	}
	for i := 0; i < 12; i++ {
		wire := <-captured
		if string(wire["max_completion_tokens"]) != "1024" || wire["max_tokens"] != nil {
			t.Fatal("SDK output limit changed")
		}
		// Script order: thinking, effort, both; each has JSON and SSE, for both models.
		mode := (i % 6) / 2
		if (wire["chat_template_kwargs"] != nil) != (mode != 1) || (wire["reasoning_effort"] != nil) != (mode != 0) {
			t.Fatalf("SDK controls lost at call %d: %s", i, wire)
		}
	}
}
