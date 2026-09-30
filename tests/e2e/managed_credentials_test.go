package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestManagedProvidersRemoteCLILifecycle(t *testing.T) {
	i := newInstance(t)
	i.server.Close()
	i.close()
	t.Setenv("TEST_MASTER", strings.Repeat("ab", 32))
	i.cfg.EncryptionKeyRef = "env:TEST_MASTER"
	i.start()
	var mu sync.Mutex
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer upstream.Close()
	for _, id := range []string{"p", "q"} {
		i.put("providers.put", map[string]any{"id": id, "base_url": upstream.URL + "/v1", "api_key": "synthetic-" + id + "-secret"})
		m := model(id, true)
		m["provider_id"] = id
		i.put("models.put", m)
		probe := i.call("models.test", map[string]string{"id": id}, 0, "")
		var data struct{ OK bool }
		if !probe.OK || json.Unmarshal(probe.Data, &data) != nil || !data.OK {
			t.Fatal("probe failed", string(probe.Data))
		}
	}
	v := i.version()
	input := map[string]any{"id": "p", "base_url": upstream.URL + "/v1", "api_key": "rotated-p-secret"}
	rotated := i.call("providers.put", input, v, "rotate-1")
	if !rotated.OK {
		t.Fatal(rotated.Error)
	}
	for _, token := range []string{"inference-secret", "invalid", ""} {
		code, b := i.request("POST", "/admin/v1/call/providers.put", token, `{"input":{"id":"p","base_url":"https://example.com","api_key":"denied-secret"},"expected_version":5,"idempotency_key":"denied"}`)
		if code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Fatal("unauthorized mutation", code)
		}
		if strings.Contains(string(b), "denied-secret") {
			t.Fatal("denied secret leaked")
		}
	}
	i.server.Close()
	i.close()
	i.start()
	replay := i.call("providers.put", input, v, "rotate-1")
	if !replay.OK || replay.Meta.OperationID != rotated.Meta.OperationID {
		t.Fatal("restart replay failed")
	}
	for _, id := range []string{"p", "q"} {
		code, b := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"`+id+`","messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 {
			t.Fatal(code, string(b))
		}
		detail := i.call("providers.get", map[string]string{"id": id}, 0, "")
		if !detail.OK {
			t.Fatal(detail.Error)
		}
		if strings.Contains(string(detail.Data), "-secret") || len(detail.Warnings) != 0 {
			t.Fatal("masked read failed")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	expected := []string{"Bearer synthetic-p-secret", "Bearer synthetic-q-secret", "Bearer rotated-p-secret", "Bearer synthetic-q-secret"}
	if len(seen) != len(expected) {
		t.Fatalf("upstream calls %d", len(seen))
	}
	for n := range expected {
		if seen[n] != expected[n] {
			t.Fatal("incorrect credential routing at call", n)
		}
	}
}
