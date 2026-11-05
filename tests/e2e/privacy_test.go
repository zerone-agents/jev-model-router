package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrivacyAgentWorkflow(t *testing.T) {
	i := newInstance(t)
	var classifications, cloudCalls atomic.Int32
	decider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
			State     json.RawMessage            `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if _, ok := req.Questions["privacy"]; ok {
			classifications.Add(1)
			if bytes.Contains(req.State, []byte("public question")) {
				w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"non_sensitive"}},"usage":{"input_tokens":11,"output_tokens":1}}`))
				return
			}
			w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"usage":{"input_tokens":11,"output_tokens":1}}`))
			return
		}
		t.Error("unexpected selection call")
		w.WriteHeader(500)
	}))
	defer decider.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer cloud.Close()
	i.provider()
	i.put("providers.put", map[string]any{"id": "cloud-provider", "base_url": cloud.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	local := model("local-model", true)
	local["location"] = "local"
	remote := model("cloud-model", true)
	remote["provider_id"] = "cloud-provider"
	i.put("models.put", local)
	i.put("models.put", remote)
	i.put("decision.put", map[string]any{"base_url": decider.URL, "model": "local-decision", "secret_ref": "env:TEST_PROVIDER_KEY", "location": "local"})
	i.put("privacy.prompt.put", map[string]any{"text": "Classify private content."})
	i.put("privacy.put", map[string]any{"enabled": true})
	for cap, expected := range map[string]string{"privacy.get": `"enabled":true`, "privacy.prompt.get": "Classify private content."} {
		result := i.call(cap, map[string]any{}, 0, "")
		if !result.OK || !bytes.Contains(result.Data, []byte(expected)) {
			t.Fatalf("%s: %s %+v", cap, result.Data, result.Error)
		}
	}
	inspect := i.call("route.inspect", map[string]any{"model": "auto", "messages": []map[string]any{{"role": "user", "content": "synthetic private value"}}}, 0, "")
	if !inspect.OK || !bytes.Contains(inspect.Data, []byte(`"privacy_outcome":"sensitive"`)) || !bytes.Contains(inspect.Data, []byte(`"model_id":"local-model"`)) {
		t.Fatalf("inspect: %s %+v", inspect.Data, inspect.Error)
	}
	for _, tc := range []struct {
		model  string
		status int
	}{{"auto", 200}, {"cloud-model", 403}} {
		status, body := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"`+tc.model+`","messages":[{"role":"user","content":"synthetic private value"}]}`)
		if status != tc.status {
			t.Fatalf("%s: %d %s", tc.model, status, body)
		}
	}
	if cloudCalls.Load() != 0 || classifications.Load() != 3 {
		t.Fatalf("cloud=%d classification=%d", cloudCalls.Load(), classifications.Load())
	}
	status, body := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"cloud-model","messages":[{"role":"user","content":"public question"}]}`)
	if status != 200 || cloudCalls.Load() != 1 || classifications.Load() != 4 {
		t.Fatalf("independent public request: status=%d cloud=%d classification=%d body=%s", status, cloudCalls.Load(), classifications.Load(), body)
	}
	status, body = i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"cloud-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/private.png"}}]}]}`)
	if status != 403 || cloudCalls.Load() != 1 || classifications.Load() != 4 {
		t.Fatalf("uninspectable image: status=%d cloud=%d classification=%d body=%s", status, cloudCalls.Load(), classifications.Load(), body)
	}
	records := i.call("records.list", map[string]any{}, 0, "")
	if !records.OK || !bytes.Contains(records.Data, []byte(`"privacy_outcome":"sensitive"`)) || !bytes.Contains(records.Data, []byte(`"invocations":1`)) {
		t.Fatalf("records: %s %+v", records.Data, records.Error)
	}
}

func TestPrivacyFallbackNeverCallsCloud(t *testing.T) {
	i := newInstance(t)
	var classification, selections, failures, cloudCalls atomic.Int32
	decider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if _, ok := req.Questions["privacy"]; ok {
			classification.Add(1)
			w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"usage":{"input_tokens":11,"output_tokens":1}}`))
			return
		}
		selections.Add(1)
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}},"usage":{"input_tokens":9,"output_tokens":1}}`))
	}))
	defer decider.Close()
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failures.Add(1)
		w.WriteHeader(503)
		w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	}))
	defer failed.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cloudCalls.Add(1); w.WriteHeader(500) }))
	defer cloud.Close()
	i.provider()
	i.put("providers.put", map[string]any{"id": "failing", "base_url": failed.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	i.put("providers.put", map[string]any{"id": "cloud-provider", "base_url": cloud.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	first := model("a-local", true)
	first["location"] = "local"
	first["provider_id"] = "failing"
	second := model("b-local", true)
	second["location"] = "local"
	remote := model("cloud-model", true)
	remote["provider_id"] = "cloud-provider"
	i.put("models.put", first)
	i.put("models.put", second)
	i.put("models.put", remote)
	i.put("decision.put", map[string]any{"base_url": decider.URL, "model": "local-decision", "secret_ref": "env:TEST_PROVIDER_KEY", "location": "local"})
	i.put("privacy.put", map[string]any{"enabled": true})
	status, body := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"auto","messages":[{"role":"user","content":"private dummy password"}]}`)
	if status != 200 || !bytes.Contains(body, []byte(`"model":"b-local"`)) {
		t.Fatalf("status %d: %s", status, body)
	}
	if classification.Load() != 1 || selections.Load() != 1 || failures.Load() != 1 || cloudCalls.Load() != 0 {
		t.Fatalf("classification=%d selection=%d failed=%d cloud=%d", classification.Load(), selections.Load(), failures.Load(), cloudCalls.Load())
	}
	records := i.call("records.list", map[string]any{}, 0, "")
	var payload struct {
		Records []struct {
			DecisionUsage struct {
				Input       int  `json:"input_tokens"`
				Output      int  `json:"output_tokens"`
				Invocations int  `json:"invocations"`
				Complete    bool `json:"complete"`
			} `json:"decision_usage"`
			Attempts []struct {
				DecisionUsage struct {
					Input       int  `json:"input_tokens"`
					Output      int  `json:"output_tokens"`
					Invocations int  `json:"invocations"`
					Complete    bool `json:"complete"`
				} `json:"decision_usage"`
			} `json:"attempts"`
		} `json:"records"`
	}
	if err := json.Unmarshal(records.Data, &payload); err != nil || len(payload.Records) != 1 {
		t.Fatalf("records=%s error=%v", records.Data, err)
	}
	rec := payload.Records[0]
	if len(rec.Attempts) != 2 || rec.DecisionUsage.Invocations != 2 || rec.DecisionUsage.Input != 20 || rec.DecisionUsage.Output != 2 || !rec.DecisionUsage.Complete {
		t.Fatalf("usage=%+v attempts=%+v", rec.DecisionUsage, rec.Attempts)
	}
	if rec.Attempts[0].DecisionUsage.Invocations != 2 || rec.Attempts[1].DecisionUsage.Invocations != 0 || rec.Attempts[0].DecisionUsage.Input+rec.Attempts[1].DecisionUsage.Input != rec.DecisionUsage.Input {
		t.Fatalf("usage reconciliation: %+v", rec.Attempts)
	}
}

func TestPrivacyMessagesAndChatStreamAtWire(t *testing.T) {
	i := newInstance(t)
	var classifierCalls, cloudCalls atomic.Int32
	classifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			State json.RawMessage `json:"state"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid classifier input")
			w.WriteHeader(400)
			return
		}
		if !bytes.Contains(request.State, []byte("SYSTEM_PRIVATE")) {
			t.Error("original system message absent")
		}
		classifierCalls.Add(1)
		w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer classifier.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cloudCalls.Add(1); w.WriteHeader(500) }))
	defer cloud.Close()
	i.provider()
	i.put("providers.put", map[string]any{"id": "cloud-provider", "base_url": cloud.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	local := model("local-model", true)
	local["location"] = "local"
	remote := model("cloud-model", true)
	remote["provider_id"] = "cloud-provider"
	i.put("models.put", local)
	i.put("models.put", remote)
	i.put("decision.put", map[string]any{"base_url": classifier.URL, "model": "local-decision", "secret_ref": "env:TEST_PROVIDER_KEY", "location": "local"})
	i.put("privacy.put", map[string]any{"enabled": true})
	for _, stream := range []bool{false, true} {
		for _, modelID := range []string{"auto", "cloud-model"} {
			body := fmt.Sprintf(`{"model":%q,"max_tokens":1024,"stream":%t,"system":"SYSTEM_PRIVATE","messages":[{"role":"user","content":"private"}]}`, modelID, stream)
			req, err := http.NewRequest("POST", i.server.URL+"/v1/messages", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer inference-secret")
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("Content-Type", "application/json")
			resp, err := i.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			result, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			code := resp.StatusCode
			if modelID == "cloud-model" {
				if code != 403 {
					t.Fatalf("messages cloud stream=%t status=%d %s", stream, code, result)
				}
				continue
			}
			if code != 200 || !bytes.Contains(result, []byte("OK")) || (stream && !bytes.Contains(result, []byte("event: message_stop"))) {
				t.Fatalf("messages auto stream=%t status=%d %s", stream, code, result)
			}
		}
	}
	code, result := i.request("POST", "/v1/chat/completions", "inference-secret", `{"model":"auto","stream":true,"messages":[{"role":"system","content":"SYSTEM_PRIVATE"},{"role":"user","content":"private"}]}`)
	if code != 200 || !bytes.Contains(result, []byte("[DONE]")) {
		t.Fatalf("chat stream status=%d %s", code, result)
	}
	if classifierCalls.Load() != 5 || cloudCalls.Load() != 0 {
		t.Fatalf("classifications=%d cloud=%d", classifierCalls.Load(), cloudCalls.Load())
	}
}

func TestPrivacyDecisionFailureAndRejectedInputAccounting(t *testing.T) {
	i := newInstance(t)
	t.Setenv("MISSING_PRIVACY_TEST_KEY", "temporary-value")
	i.provider()
	local := model("local-model", true)
	local["location"] = "local"
	i.put("models.put", local)
	i.put("decision.put", map[string]any{"base_url": "http://127.0.0.1:1", "model": "local-decision", "secret_ref": "env:MISSING_PRIVACY_TEST_KEY", "location": "local"})
	i.put("privacy.put", map[string]any{"enabled": true})
	if err := os.Unsetenv("MISSING_PRIVACY_TEST_KEY"); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"model":"local-model","messages":[{"role":"user","content":"synthetic private"}]}`,
		`{"model":"local-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image"}}]}]}`,
	} {
		code, result := i.request("POST", "/v1/chat/completions", "inference-secret", body)
		if code != 200 {
			t.Fatalf("status=%d body=%s", code, result)
		}
	}
	result := i.call("records.list", map[string]any{}, 0, "")
	var page struct {
		Records []struct {
			Outcome     string `json:"privacy_outcome"`
			Failure     string `json:"privacy_failure"`
			Restriction string `json:"privacy_restriction"`
			Usage       struct {
				Invocations int  `json:"invocations"`
				Complete    bool `json:"complete"`
			} `json:"decision_usage"`
			Attempts []struct {
				Usage struct {
					Invocations int  `json:"invocations"`
					Complete    bool `json:"complete"`
				} `json:"decision_usage"`
			} `json:"attempts"`
		} `json:"records"`
	}
	if err := json.Unmarshal(result.Data, &page); err != nil || len(page.Records) != 2 {
		t.Fatalf("records=%s err=%v", result.Data, err)
	}
	seenFailed, seenPreflight := false, false
	for _, record := range page.Records {
		if record.Outcome != "uncertain" || record.Restriction != "local_only" || len(record.Attempts) != 1 {
			t.Fatalf("record=%+v", record)
		}
		switch record.Usage.Invocations {
		case 1:
			seenFailed = true
			if record.Usage.Complete || record.Failure != "classification_unavailable" || record.Attempts[0].Usage.Invocations != 1 || record.Attempts[0].Usage.Complete {
				t.Fatalf("failure accounting=%+v", record)
			}
		case 0:
			seenPreflight = true
			if !record.Usage.Complete || record.Attempts[0].Usage.Invocations != 0 || !record.Attempts[0].Usage.Complete {
				t.Fatalf("preflight accounting=%+v", record)
			}
		default:
			t.Fatalf("unexpected invocations=%d", record.Usage.Invocations)
		}
	}
	if !seenFailed || !seenPreflight {
		t.Fatalf("missing records: %s", result.Data)
	}
	if bytes.Contains(result.Data, []byte("MISSING_PRIVACY_TEST_KEY")) {
		t.Fatal("credential reference leaked")
	}
}
