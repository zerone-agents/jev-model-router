package e2e

import (
	"bytes"
	"context"
	"github.com/zerone-agents/jev-model-router/internal/transport/cli"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAgentHealthWorkflow(t *testing.T) {
	i := newInstance(t)
	var fail atomic.Bool
	fail.Store(true)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fail.Load() {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"quota"}}`))
			return
		}
		w.Write([]byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
	}))
	defer up.Close()
	i.put("providers.put", map[string]any{"id": "p", "base_url": up.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	i.put("models.put", model("m", true))
	var out, errout bytes.Buffer
	if cli.Run(context.Background(), []string{"schema", "models.get", "--url", i.server.URL}, nil, &out, &errout) != 0 || !bytes.Contains(out.Bytes(), []byte(`"health"`)) {
		t.Fatalf("discovery: %s", out.String())
	}
	before := i.version()
	r := i.call("models.test", map[string]any{"id": "m"}, 0, "")
	if !r.OK || !bytes.Contains(r.Data, []byte(`"state":"isolated"`)) {
		t.Fatalf("probe: %s %v", r.Data, r.Error)
	}
	r = i.call("route.inspect", map[string]any{"model": "auto", "messages": []map[string]string{{"role": "user", "content": "hi"}}}, 0, "")
	if r.OK || r.Error == nil || r.Error.Code != "no_candidates" || len(r.Error.HealthExclusions) != 1 {
		t.Fatalf("inspect: %+v", r.Error)
	}
	fail.Store(false)
	r = i.call("models.test", map[string]any{"id": "m"}, 0, "")
	if !r.OK || !bytes.Contains(r.Data, []byte(`"state":"healthy"`)) || !bytes.Contains(r.Data, []byte(`"persisted":true`)) {
		t.Fatalf("recovery: %s", r.Data)
	}
	if i.version() != before {
		t.Fatal("health changed config version")
	}
}
