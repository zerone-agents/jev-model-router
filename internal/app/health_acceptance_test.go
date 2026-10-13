package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHealthRestartAndExplicitRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"quota", 429, `{"error":{"code":"insufficient_quota","message":"quota"}}`},
		{"kimi_access_terminated", 403, `{"error":{"type":"access_terminated_error","message":"weekly usage limit reached"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) { testHealthRestartAndExplicitRecovery(t, tc.status, tc.body) })
	}
}

func testHealthRestartAndExplicitRecovery(t *testing.T, status int, body string) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "inference-test")
	t.Setenv("HEALTH_TEST_KEY", "provider-test")
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if fail.Load() {
			w.WriteHeader(status)
			w.Write([]byte(body))
			return
		}
		w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"up","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	cfg, e := LoadConfig("", env(nil))
	if e != nil {
		t.Fatal(e)
	}
	cfg.Database = filepath.Join(t.TempDir(), "router.db")
	h, closeHandler, e := Handler(cfg)
	if e != nil {
		t.Fatal(e)
	}
	post := func(path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	b, _ := json.Marshal(map[string]any{"input": map[string]any{"id": "p", "base_url": upstream.URL + "/v1", "secret_ref": "env:HEALTH_TEST_KEY"}, "expected_version": 1, "idempotency_key": "p"})
	if w := post("/admin/v1/call/providers.put", "settings-test", string(b)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := post("/admin/v1/call/models.put", "settings-test", `{"input":{"id":"m","provider_id":"p","upstream_name":"up","enabled":true,"location":"cloud","description":"test","capabilities":{"context_limit":8192,"tools":false,"images":false,"structured_output":false}},"expected_version":2,"idempotency_key":"m"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	chat := func(model string) *httptest.ResponseRecorder {
		return post("/v1/chat/completions", "inference-test", `{"model":"`+model+`","messages":[{"role":"user","content":"hello"}]}`)
	}
	if w := chat("auto"); w.Code != status {
		t.Fatalf("first upstream: %d %s", w.Code, w.Body)
	}
	if w := chat("auto"); w.Code != 422 || !strings.Contains(w.Body.String(), "health_exclusions") {
		t.Fatalf("not filtered: %d %s", w.Code, w.Body)
	}
	if calls.Load() != 1 {
		t.Fatal("isolated target called by auto")
	}
	closeHandler()
	h, closeHandler, e = Handler(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer closeHandler()
	if w := chat("auto"); w.Code != 422 {
		t.Fatalf("restart lost isolation: %d %s", w.Code, w.Body)
	}
	fail.Store(false)
	if w := chat("m"); w.Code != 200 {
		t.Fatalf("explicit blocked: %d %s", w.Code, w.Body)
	}
	if w := chat("auto"); w.Code != 200 {
		t.Fatalf("not recovered: %d %s", w.Code, w.Body)
	}
	w := post("/admin/v1/call/models.get", "settings-test", `{"input":{"id":"m"}}`)
	if !strings.Contains(w.Body.String(), `"state":"healthy"`) || !strings.Contains(w.Body.String(), `"version":3`) {
		t.Fatal(w.Body.String())
	}
}
