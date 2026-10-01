package httptransport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// This exercises the production HTTP, planner, executor and Bifrost adapter.
// Only the generation endpoint is simulated.
func outputLimitRouter(t *testing.T) (http.Handler, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["max_completion_tokens"]) != "1024" || body["max_tokens"] != nil || string(body["model"]) != `"gpt-4o-mini"` {
			t.Errorf("upstream limit/model changed: %s", body)
		}
		if string(body["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"test\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"test","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
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
	cfg.Models[0].UpstreamName = "gpt-4o-mini"
	store := testStore{cfg}
	h := NewHandler(management.New(store, nil), store, &routing.Planner{Check: provider.Check}, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	return h, calls
}

func TestSDKOutputLimitThroughRouter(t *testing.T) {
	h, calls := outputLimitRouter(t)
	for _, model := range []string{"auto", "external"} {
		for _, field := range []string{"max_tokens", "max_completion_tokens"} {
			for _, stream := range []bool{false, true} {
				extra := ""
				if stream {
					extra = `,"stream":true,"stream_options":{"include_usage":true}`
				}
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"system","content":"Reply briefly."},{"role":"user","content":"请用一句中文解释模型路由。"}],%q:1024%s}`, model, field, extra)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "ok") {
					t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
				}
				if stream && !strings.Contains(w.Body.String(), "[DONE]") {
					t.Fatal("incomplete stream")
				}
			}
		}
	}
	if calls.Load() != 8 {
		t.Fatalf("calls=%d", calls.Load())
	}
	for _, extra := range []string{`"max_tokens":null`, `"max_tokens":8`, `"max_tokens":1024,"max_completion_tokens":1024`, `"max_tokens":1024,"reasoning_effort":"SECRET"`, `"stream":"SECRET"`} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}],`+extra+`}`)))
		if w.Code != 400 || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 8 {
		t.Fatal("invalid request reached provider")
	}
}

// Opt-in verification with an unmodified local SDK checkout; no SDK dependency
// is downloaded by CI. The upstream remains a local simulated endpoint.
func TestUnmodifiedAgentSDK(t *testing.T) {
	sdk := os.Getenv("JEV_TEST_AGENT_SDK")
	if sdk == "" {
		t.Skip("set JEV_TEST_AGENT_SDK to an agent-sdk checkout with tsx installed")
	}
	h, calls := outputLimitRouter(t)
	server := httptest.NewServer(h)
	defer server.Close()
	script, err := filepath.Abs("../../../scripts/verify-agent-sdk.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(sdk, "node_modules/.bin/tsx"), script, sdk, server.URL+"/v1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SDK verification: %v\n%s", err, out)
	}
	t.Log(string(out))
	if calls.Load() != 4 {
		t.Fatalf("SDK calls=%d", calls.Load())
	}
}
