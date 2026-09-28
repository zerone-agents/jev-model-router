package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/app"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/transport/cli"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type instance struct {
	t        *testing.T
	cfg      app.Config
	server   *httptest.Server
	close    func()
	calls    atomic.Int32
	upstream *httptest.Server
}

func newInstance(t *testing.T) *instance {
	t.Helper()
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-secret")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "inference-secret")
	t.Setenv("TEST_PROVIDER_KEY", "upstream-secret")
	cfg, e := app.LoadConfig("", func(string) (string, bool) { return "", false })
	if e != nil {
		t.Fatal(e)
	}
	cfg.Database = filepath.Join(t.TempDir(), "router.sqlite")
	i := &instance{t: t, cfg: cfg}
	i.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.calls.Add(1)
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/v1/systemone" {
			w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}},"usage":{"input_tokens":15,"output_tokens":1}}`))
			return
		}
		if string(body["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	i.start()
	t.Cleanup(func() { i.server.Close(); i.close(); i.upstream.Close() })
	return i
}
func (i *instance) start() {
	h, close, e := app.Handler(i.cfg)
	if e != nil {
		i.t.Fatal(e)
	}
	i.close = close
	i.server = httptest.NewServer(h)
}
func (i *instance) call(cap string, input any, version int64, key string) management.Result {
	i.t.Helper()
	b, _ := json.Marshal(input)
	args := []string{"call", cap, "--url", i.server.URL, "--json", "-"}
	if version > 0 {
		args = append(args, "--expected-version", fmt.Sprint(version), "--idempotency-key", key)
	}
	var out, err bytes.Buffer
	code := cli.Run(context.Background(), args, bytes.NewReader(b), &out, &err)
	var r management.Result
	if json.Unmarshal(out.Bytes(), &r) != nil {
		i.t.Fatalf("invalid CLI %d %s", code, out.String())
	}
	if strings.Contains(out.String()+err.String(), "-secret") {
		i.t.Fatal("secret leaked")
	}
	return r
}
func (i *instance) version() int64 {
	r := i.call("status.get", map[string]any{}, 0, "")
	var d struct{ Version int64 }
	json.Unmarshal(r.Data, &d)
	return d.Version
}
func (i *instance) put(cap string, input any) management.Result {
	r := i.call(cap, input, i.version(), fmt.Sprintf("key-%d", time.Now().UnixNano()))
	if !r.OK {
		i.t.Fatalf("%s: %+v", cap, r.Error)
	}
	return r
}
func model(id string, enabled bool) map[string]any {
	return map[string]any{"id": id, "provider_id": "p", "upstream_name": "gpt-4o-mini", "description": "Balanced general model", "location": "cloud", "enabled": enabled, "capabilities": map[string]any{"context_limit": 32000, "tools": true, "images": true, "structured_output": true}}
}
func (i *instance) provider() {
	i.put("providers.put", map[string]any{"id": "p", "base_url": i.upstream.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
}
func (i *instance) request(method, path, token, body string) (int, []byte) {
	i.t.Helper()
	r, _ := http.NewRequest(method, i.server.URL+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	resp, e := i.server.Client().Do(r)
	if e != nil {
		i.t.Fatal(e)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}
func TestFreshInstanceAgentWorkflow(t *testing.T) {
	i := newInstance(t)
	status := i.call("status.get", map[string]any{}, 0, "")
	if !bytes.Contains(status.Data, []byte(`"ready":false`)) {
		t.Fatal(string(status.Data))
	}
	var out, errs bytes.Buffer
	if cli.Run(context.Background(), []string{"schema", "--url", i.server.URL}, nil, &out, &errs) != 0 {
		t.Fatal(out.String())
	}
	var catalog struct {
		Data []struct {
			ID           string
			OutputSchema map[string]any `json:"output_schema"`
		}
	}
	json.Unmarshal(out.Bytes(), &catalog)
	for _, c := range catalog.Data {
		if c.OutputSchema["properties"] == nil {
			t.Errorf("%s output contract is not described", c.ID)
		}
	}
	i.provider()
	i.put("decision.put", map[string]any{"base_url": i.upstream.URL, "model": "jev-1.13.0", "secret_ref": "env:TEST_PROVIDER_KEY"})
	i.put("models.put", model("fast", false))
	probe := i.call("models.test", map[string]any{"id": "fast"}, 0, "")
	if !probe.OK || !bytes.Contains(probe.Data, []byte(`"ok":true`)) {
		t.Fatal(string(probe.Data), probe.Error)
	}
	i.put("models.put", model("fast", true))
	i.put("models.put", model("deep", true))
	before := i.version()
	inspect := i.call("route.inspect", map[string]any{"model": "auto", "messages": []map[string]any{{"role": "user", "content": "hi"}}}, 0, "")
	if !inspect.OK {
		t.Fatal(inspect.Error)
	}
	for _, m := range []string{"auto", "fast"} {
		for _, stream := range []bool{false, true} {
			code, b := i.request("POST", "/v1/chat/completions", "inference-secret", fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"stream":%t}`, m, stream))
			if code != 200 || (!bytes.Contains(b, []byte(`"model":"fast"`)) && !bytes.Contains(b, []byte(`"model":"deep"`))) {
				t.Fatalf("%d %s", code, b)
			}
			if stream && !bytes.Contains(b, []byte("[DONE]")) {
				t.Fatal(string(b))
			}
		}
	}
	code, list := i.request("GET", "/v1/models", "inference-secret", "")
	if code != 200 || bytes.Count(list, []byte(`"id":"auto"`)) != 1 {
		t.Fatal(string(list))
	}
	records := i.call("records.list", map[string]any{}, 0, "")
	if !records.OK || !bytes.Contains(records.Data, []byte("model_id")) {
		t.Fatal(records.Error, string(records.Data))
	}
	for _, v := range []struct{ path, token, method, body string }{{"/admin/v1/schema", "inference-secret", "GET", ""}, {"/v1/chat/completions", "settings-secret", "POST", `{}`}} {
		if code, _ := i.request(v.method, v.path, v.token, v.body); code != 403 {
			t.Fatal(code)
		}
	}
	i.server.Close()
	i.close()
	i.start()
	if i.version() != before {
		t.Fatal("configuration lost")
	}
}
func TestReservedIDAllEntryPoints(t *testing.T) {
	i := newInstance(t)
	i.provider()
	for _, enabled := range []bool{true, false} {
		version := i.version()
		r := i.call("models.put", model("auto", enabled), version, "reserved")
		if r.OK || i.version() != version {
			t.Fatal("CLI auto accepted")
		}
		b, _ := json.Marshal(management.Call{Input: mustJSON(model("auto", enabled)), ExpectedVersion: &version, IdempotencyKey: "reserved-http"})
		code, _ := i.request("POST", "/admin/v1/call/models.put", "settings-secret", string(b))
		if code != 400 || i.version() != version {
			t.Fatal("HTTP auto accepted")
		}
	}
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func TestNoHiddenNetworkOnStatus(t *testing.T) {
	i := newInstance(t)
	i.provider()
	before := i.calls.Load()
	i.call("status.get", map[string]any{}, 0, "")
	i.request("GET", "/admin/v1/schema", "settings-secret", "")
	if i.calls.Load() != before {
		t.Fatal("hidden network")
	}
}
func TestIdempotentCLIResponseLoss(t *testing.T) {
	i := newInstance(t)
	version := i.version()
	input := map[string]any{"id": "p", "base_url": i.upstream.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"}
	realServer := i.server
	captured := make(chan management.Result, 1)
	drop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream, _ := http.NewRequestWithContext(r.Context(), r.Method, realServer.URL+r.URL.Path, r.Body)
		upstream.Header = r.Header.Clone()
		response, e := realServer.Client().Do(upstream)
		if e != nil {
			t.Error(e)
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		var result management.Result
		json.NewDecoder(response.Body).Decode(&result)
		captured <- result
		connection, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			t.Error(e)
			return
		}
		connection.Close()
	}))
	i.server = drop
	lost := i.call("providers.put", input, version, "lost")
	i.server = realServer
	drop.Close()
	original := <-captured
	if lost.OK || !original.OK {
		t.Fatal("did not lose successful response")
	}
	i.server.Close()
	i.close()
	i.start()
	replay := i.call("providers.put", input, version, "lost")
	if !replay.OK || replay.Meta.OperationID != original.Meta.OperationID || i.version() != version+1 {
		t.Fatal("replayed write changed result")
	}
}
