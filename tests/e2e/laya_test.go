package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/transport/cli"
)

// The same Agent workflow runs against a local deterministic choice server by
// default, or the actual offline Laya service when LAYA_E2E_URL is set.
func TestLocalLayaAgentWorkflow(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			i := newInstance(t)
			send := func(method, path, token, body string) (int, []byte) {
				req, _ := http.NewRequest(method, i.server.URL+path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("anthropic-version", "2023-06-01")
				response, err := i.server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, _ := io.ReadAll(response.Body)
				return response.StatusCode, data
			}
			var decisions atomic.Int32
			endpoint := os.Getenv("LAYA_E2E_URL")
			token := os.Getenv("LAYA_E2E_TOKEN")
			var unavailable atomic.Bool
			decider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				decisions.Add(1)
				if unavailable.Load() {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":{"code":"model_unavailable","message":"Not ready."}}`)
					return
				}
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != "/v1/systemone" {
					t.Error("incorrect decision path")
				}
				if endpoint != "" {
					req, _ := http.NewRequestWithContext(r.Context(), "POST", strings.TrimRight(endpoint, "/")+r.URL.Path, bytes.NewReader(body))
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Error(err)
						w.WriteHeader(502)
						return
					}
					defer resp.Body.Close()
					w.WriteHeader(resp.StatusCode)
					io.Copy(w, resp.Body)
					return
				}
				var request struct {
					Model     string
					Questions map[string]struct{ Criteria map[string]string }
				}
				if err := json.Unmarshal(body, &request); err != nil {
					t.Error(err)
				}
				if request.Model != "laya-multilingual" {
					t.Error("wrong alias")
				}
				answers := map[string]any{}
				for name, question := range request.Questions {
					choice := "m0"
					if name == "effort" {
						choice = "xhigh"
					}
					if _, ok := question.Criteria[choice]; !ok {
						t.Errorf("choice missing: %s", name)
					}
					answers[name] = map[string]string{"type": "choice", "choice": choice}
				}
				json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}})
			}))
			defer decider.Close()
			i.upstream.Close()
			var failNext atomic.Bool
			var mu sync.Mutex
			var seenModels []string
			messages := `[ {"role":"user","content":"Translate hello into Chinese."}, {"role":"assistant","content":"你好"}, {"role":"user","content":"Translate goodbye too."} ]`
			i.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				var want, got any
				json.Unmarshal([]byte(messages), &want)
				json.Unmarshal(body["messages"], &got)
				a, _ := json.Marshal(want)
				b, _ := json.Marshal(got)
				if !bytes.Equal(a, b) {
					t.Error("generation history changed")
				}
				var name string
				json.Unmarshal(body["model"], &name)
				mu.Lock()
				seenModels = append(seenModels, name)
				mu.Unlock()
				if failNext.Swap(false) {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":{"code":"service_unavailable","message":"Synthetic transient failure."}}`)
					return
				}
				if protocol == "anthropic" {
					if r.URL.Path != "/v1/messages" {
						t.Error("not native Messages")
					}
					fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"再见"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`)
				} else {
					fmt.Fprint(w, `{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"再见"},"finish_reason":"stop"}]}`)
				}
			}))
			var out, errs bytes.Buffer
			if cli.Run(context.Background(), []string{"schema", "decision.put", "--url", i.server.URL}, nil, &out, &errs) != 0 || !strings.Contains(out.String(), "base_url") {
				t.Fatal("decision schema unavailable")
			}
			config := map[string]any{"base_url": decider.URL, "path": "/v1/systemone", "model": "laya-multilingual", "secret_ref": "env:TEST_PROVIDER_KEY"}
			version := i.version()
			if !i.call("decision.put", config, version, "laya-config").OK {
				t.Fatal("versioned configuration failed")
			}
			if !i.call("decision.put", config, version, "laya-config").OK {
				t.Fatal("configuration replay failed")
			}
			if decisions.Load() != 0 {
				t.Fatal("config write called model")
			}
			read := i.call("decision.get", map[string]any{}, 0, "")
			if !read.OK || !bytes.Contains(read.Data, []byte("laya-multilingual")) {
				t.Fatal("decision readback failed")
			}
			i.put("providers.put", map[string]any{"id": "p", "base_url": i.upstream.URL + "/v1", "protocol": protocol, "secret_ref": "env:TEST_PROVIDER_KEY"})
			for _, id := range []string{"fast", "deep"} {
				m := model(id, true)
				m["upstream_name"] = id
				m["description"] = "Short text and translation."
				m["capabilities"].(map[string]any)["reasoning"] = []map[string]string{{"reasoning_effort": "low"}, {"reasoning_effort": "high"}}
				i.put("models.put", m)
			}
			inspect := i.call("route.inspect", map[string]any{"model": "auto", "messages": []map[string]string{{"role": "user", "content": "Translate hello."}}}, 0, "")
			if !inspect.OK || !bytes.Contains(inspect.Data, []byte("effective_effort")) {
				t.Fatal("inspect failed", inspect.Error, string(inspect.Data))
			}
			path := "/v1/chat/completions"
			extra := ""
			if protocol == "anthropic" {
				path = "/v1/messages"
				extra = `,"max_tokens":128`
			}
			before := decisions.Load()
			failNext.Store(true)
			code, body := send("POST", path, "inference-secret", `{"model":"auto","messages":`+messages+extra+`}`)
			if code != 200 {
				t.Fatalf("generation %d %s", code, body)
			}
			if decisions.Load() != before+2 {
				t.Fatalf("fallback did not use local decisions twice: %d", decisions.Load()-before)
			}
			mu.Lock()
			targets := append([]string(nil), seenModels...)
			mu.Unlock()
			if len(targets) != 2 || targets[0] == targets[1] {
				t.Fatalf("fallback target exclusion failed: %v", targets)
			}
			records := i.call("records.list", map[string]any{}, 0, "")
			if !records.OK || !bytes.Contains(records.Data, []byte("effective_effort")) {
				t.Fatal("effort not recorded")
			}
			unavailable.Store(true)
			before = decisions.Load()
			code, body = send("POST", path, "inference-secret", `{"model":"fast","messages":`+messages+extra+`}`)
			if code != 200 || decisions.Load() != before {
				t.Fatalf("explicit route depended on decider: %d %s", code, body)
			}
			if !i.call("status.get", map[string]any{}, 0, "").OK {
				t.Fatal("settings unavailable")
			}
			code, body = send("POST", path, "inference-secret", `{"model":"auto","messages":`+messages+extra+`}`)
			if code == 200 || !bytes.Contains(body, []byte("model_unavailable")) {
				t.Fatalf("decider failure not preserved: %d %s", code, body)
			}
		})
	}
}
