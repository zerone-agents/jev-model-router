package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestManagedDecisionRemoteCLI(t *testing.T) {
	i := newInstance(t)
	i.server.Close()
	i.close()
	t.Setenv("TEST_MASTER", strings.Repeat("ab", 32))
	i.cfg.EncryptionKeyRef = "env:TEST_MASTER"
	i.start()
	var mu sync.Mutex
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/decisions" {
			t.Error("wrong path")
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}}}`))
	}))
	defer upstream.Close()
	i.provider()
	i.put("models.put", model("a", true))
	i.put("models.put", model("b", true))
	for _, key := range []string{"first-decision-secret", "rotated-decision-secret"} {
		i.put("decision.put", map[string]any{"base_url": upstream.URL, "path": "/api/v1/decisions", "model": "typesafe/jev-1.13", "api_key": key})
		i.server.Close()
		i.close()
		i.start()
		read := i.call("decision.get", map[string]any{}, 0, "")
		if !read.OK || !strings.Contains(string(read.Data), "managed:") {
			t.Fatal("decision read failed", string(read.Data))
		}
		code, body := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"auto","messages":[{"role":"user","content":"hello"}]}`)
		var response struct{ Choices []any }
		if code != 200 || json.Unmarshal(body, &response) != nil || len(response.Choices) == 0 {
			t.Fatal("auto failed", code, string(body))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer first-decision-secret" || seen[1] != "Bearer rotated-decision-secret" {
		t.Fatal("decision credentials did not survive rotation/restart")
	}
}
