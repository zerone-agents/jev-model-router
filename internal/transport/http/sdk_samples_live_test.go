package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type sampleWriter struct {
	http.ResponseWriter
	status int
}

func (w *sampleWriter) WriteHeader(c int)           { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *sampleWriter) Flush()                      { w.ResponseWriter.(http.Flusher).Flush() }
func (w *sampleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// TestSDKLiveSamples invokes unmodified examples against a real generation endpoint.
// It is opt-in because samples execute tools and incur provider charges.
func TestSDKLiveSamples(t *testing.T) {
	if os.Getenv("JEV_RUN_LIVE_SAMPLES") == "" {
		t.Skip("opt in")
	}
	sdk := os.Getenv("JEV_TEST_AGENT_SDK")
	if sdk == "" {
		t.Fatal("JEV_TEST_AGENT_SDK is required")
	}
	key, e := os.ReadFile(os.Getenv("JEV_LIVE_KEY_FILE"))
	if e != nil {
		t.Fatal("missing key")
	}
	g, e := provider.New(func(string) ([]byte, error) { return bytes.TrimSpace(key), nil })
	if e != nil {
		t.Fatal(e)
	}
	defer g.(io.Closer).Close()
	cfg := snapshot()
	cfg.Providers[0].BaseURL = os.Getenv("JEV_LIVE_BASE_URL")
	cfg.Models[0].UpstreamName = os.Getenv("JEV_LIVE_MODEL")
	if cfg.Providers[0].BaseURL == "" || cfg.Models[0].UpstreamName == "" {
		t.Fatal("JEV_LIVE_BASE_URL and JEV_LIVE_MODEL are required")
	}
	cfg.Models[0].Capabilities = routing.Capabilities{ContextLimit: 1000000, Tools: true, StructuredOutput: true, Reasoning: []routing.ReasoningCombination{{EnableThinking: new(true)}, {Effort: "low"}, {Effort: "medium"}, {Effort: "high"}}}
	store := testStore{cfg}
	h := NewHandler(management.New(store, nil), store, &routing.Planner{PrepareCheck: provider.PrepareCheck}, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	for _, sample := range []string{"basic/01-simple-query.ts", "basic/02-multi-tool.ts", "basic/03-multi-turn.ts", "basic/04-prompt-api.ts", "basic/05-custom-system-prompt.ts", "tools/07-custom-tools.ts", "streaming/16-streaming.ts", "streaming/17-streaming-with-tools.ts", "advanced/13-hooks.ts", "advanced/15-openai-compat.ts", "advanced/32-reasoning-effort.ts", "sessions/31-session-query-limit.ts"} {
		t.Run(sample, func(t *testing.T) {
			// These two examples use fixed /tmp paths instead of their workdir.
			fixed := map[string]string{"basic/03-multi-turn.ts": "/tmp/oas-test.txt", "streaming/17-streaming-with-tools.ts": "/tmp/sdk-test-output.ts"}[sample]
			if fixed != "" {
				if _, err := os.Lstat(fixed); !os.IsNotExist(err) {
					t.Fatalf("sample target must not already exist: %s", fixed)
				}
				defer os.Remove(fixed)
			}
			var calls, failures, results atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(b))
				var req struct{ Messages []struct{ Role string } }
				json.Unmarshal(b, &req)
				for _, m := range req.Messages {
					if m.Role == "tool" {
						results.Add(1)
					}
				}
				calls.Add(1)
				sw := &sampleWriter{ResponseWriter: w, status: 200}
				h.ServeHTTP(sw, r)
				if sw.status != 200 {
					failures.Add(1)
					t.Logf("HTTP %d", sw.status)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"router-sample-fixture","version":"1.0.0"}`), 0600)
			os.Mkdir(filepath.Join(dir, "src"), 0700)
			os.WriteFile(filepath.Join(dir, "src/agent.ts"), []byte("export const sample = true;\n"), 0600)
			ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "--require", filepath.Join(sdk, "node_modules/tsx/dist/preflight.cjs"), "--import", filepath.Join(sdk, "node_modules/tsx/dist/loader.mjs"), filepath.Join(sdk, "examples", sample))
			cmd.WaitDelay = 5 * time.Second
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "ZERONE_AGENT_API_TYPE=openai-completions", "ZERONE_AGENT_API_KEY=local-test", "ZERONE_AGENT_MODEL=auto", "ZERONE_AGENT_BASE_URL="+server.URL+"/v1")
			out, err := cmd.CombinedOutput()
			if logs := os.Getenv("JEV_SAMPLE_LOG_DIR"); logs != "" {
				if err := os.MkdirAll(logs, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(logs, filepath.Base(sample)+".log"), out, 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("requests=%d tool_history=%d failures=%d output_bytes=%d", calls.Load(), results.Load(), failures.Load(), len(out))
			if err != nil || calls.Load() == 0 || failures.Load() > 0 || bytes.Contains(out, []byte("subtype: error")) || bytes.Contains(out, []byte("API error")) || bytes.Contains(out, []byte("--- error")) || bytes.Contains(out, []byte("Result: error")) {
				t.Fatalf("sample failed: %v (inspect optional local logs)", err)
			}
			if (strings.Contains(sample, "01-") || strings.Contains(sample, "02-") || strings.Contains(sample, "04-") || strings.Contains(sample, "07-")) && results.Load() == 0 {
				t.Fatal("no actual tool round trip")
			}
		})
	}
}
