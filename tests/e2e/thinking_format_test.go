package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderThinkingFormatThroughCLI(t *testing.T) {
	i := newInstance(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["enable_thinking"]) != "false" || body["chat_template_kwargs"] != nil {
			t.Errorf("wrong thinking encoding: %s", body)
		}
		fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	i.put("providers.put", map[string]any{
		"id": "p", "base_url": upstream.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY", "thinking_format": "top_level",
	})
	result := i.call("providers.get", map[string]string{"id": "p"}, 0, "")
	if !result.OK || !strings.Contains(string(result.Data), `"thinking_format":"top_level"`) {
		t.Fatalf("provider format did not round-trip: %s", result.Data)
	}
	i.put("models.put", model("example", true))
	status, body := i.request("POST", "/v1/chat/completions", "inference-secret",
		`{"model":"example","messages":[{"role":"user","content":"hello"}],"enable_thinking":false,"chat_template_kwargs":{"enable_thinking":false}}`)
	if status != 200 {
		t.Fatalf("inference failed: %d %s", status, body)
	}
}
