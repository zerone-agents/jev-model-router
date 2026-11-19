package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailureCaptureNativeToolStream(t *testing.T) {
	instance := newInstance(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []struct{ name, body string }{
			{"message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"upstream","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"before tool"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","name":"webReader","id":"s1","input":{"url":"https://example.com/private-token"}}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":1}`},
		} {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame.name, frame.body)
			w.(http.Flusher).Flush()
		}
	}))
	defer up.Close()
	instance.put("providers.put", map[string]any{"id": "p", "base_url": up.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY", "protocol": "anthropic"})
	instance.put("models.put", model("native", true))
	req, err := http.NewRequest("POST", instance.server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"native","stream":true,"messages":[{"role":"user","content":"read a page"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer inference-secret")
	req.Header.Set("Content-Type", "application/json")
	response, err := instance.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(wire), "before tool") || !strings.Contains(string(wire), "upstream_error") {
		t.Fatalf("unexpected response %d: %s (%v)", response.StatusCode, wire, err)
	}
	capture := instance.call("records.capture.get", map[string]string{"request_id": response.Header.Get("X-Request-ID")}, 0, "")
	var exported struct {
		Evidence struct {
			Upstream struct {
				Exchanges []struct {
					Origin   string `json:"upstream_origin"`
					Elapsed  *int64 `json:"elapsed_ms"`
					Attempt  int    `json:"attempt"`
					Protocol string `json:"protocol"`
				} `json:"exchanges"`
			} `json:"upstream"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(capture.Data, &exported); err != nil {
		t.Fatal(err)
	}
	exchanges := exported.Evidence.Upstream.Exchanges
	if len(exchanges) != 1 || exchanges[0].Origin != up.URL || exchanges[0].Attempt != 1 ||
		exchanges[0].Protocol != "messages" || exchanges[0].Elapsed == nil || *exchanges[0].Elapsed < 0 {
		t.Fatalf("invalid upstream exchange metadata: %+v", exchanges)
	}

	evidence := string(capture.Data)
	if !capture.OK || !strings.Contains(evidence, "server_tool_use") || !strings.Contains(evidence, `"termination":"eof"`) ||
		!strings.Contains(evidence, `"response":{"sse"`) || strings.Contains(evidence, "private-token") {
		t.Fatalf("unexpected capture: %+v %s", capture.Error, evidence)
	}
}

func TestFailureCaptureOpenAIHTTPError(t *testing.T) {
	instance := newInstance(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"private-token","type":"rate_limit_error"},"usage":{"input_tokens":7}}`)
	}))
	defer up.Close()
	instance.put("providers.put", map[string]any{"id": "p", "base_url": up.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY"})
	instance.put("models.put", model("openai", true))
	for _, stream := range []bool{false, true} {
		req, err := http.NewRequest("POST", instance.server.URL+"/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"openai","stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer inference-secret")
		req.Header.Set("Content-Type", "application/json")
		res, err := instance.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("unexpected HTTP result: %d (%v)", res.StatusCode, err)
		}
		result := instance.call("records.capture.get", map[string]string{"request_id": res.Header.Get("X-Request-ID")}, 0, "")
		evidence := string(result.Data)
		if !result.OK || !strings.Contains(evidence, `"status":429`) || strings.Contains(evidence, "private-token") ||
			!strings.Contains(evidence, `"input_tokens":7`) || strings.Contains(evidence, `"response_unavailable":true`) || !strings.Contains(evidence, `"parsed_error_type":"rate_limit_error"`) {
			t.Fatalf("stream=%v: missing safe upstream evidence: %s", stream, evidence)
		}
	}
}
