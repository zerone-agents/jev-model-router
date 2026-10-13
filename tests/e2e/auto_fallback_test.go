package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentAutoFallback(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, messages := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("native=%v/messages=%v/stream=%v", native, messages, stream), func(t *testing.T) {
					i := newInstance(t)
					var calls atomic.Int32
					up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var b struct {
							Model  string
							Stream bool
						}
						json.NewDecoder(r.Body).Decode(&b)
						calls.Add(1)
						if b.Model == "broken" {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(503)
							fmt.Fprint(w, `{"error":{"code":"overloaded_error","message":"busy"}}`)
							return
						}
						if native {
							w.Header().Set("Content-Type", "application/json")
							if !b.Stream {
								fmt.Fprint(w, `{"id":"success","type":"message","role":"assistant","model":"working","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
								return
							}
							w.Header().Set("Content-Type", "text/event-stream")
							events := []struct{ kind, body string }{
								{"message_start", `{"type":"message_start","message":{"id":"success","type":"message","role":"assistant","model":"working","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`},
								{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
								{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`},
								{"content_block_stop", `{"type":"content_block_stop","index":0}`},
								{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`},
								{"message_stop", `{"type":"message_stop"}`},
							}
							for _, e := range events {
								fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.kind, e.body)
							}
							return
						}
						if b.Stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: {\"id\":\"success\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
							return
						}
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"success","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
					}))
					defer up.Close()
					i.put("providers.put", map[string]any{"id": "p", "base_url": up.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY", "protocol": map[bool]string{false: "openai", true: "anthropic"}[native]})
					for _, id := range []string{"a", "b", "c"} {
						m := model(id, true)
						m["upstream_name"] = "working"
						if id == "a" {
							m["upstream_name"] = "broken"
						}
						i.put("models.put", m)
					}
					i.put("decision.put", map[string]any{"base_url": i.upstream.URL, "model": "jev", "secret_ref": "env:TEST_PROVIDER_KEY"})
					path := "/v1/chat/completions"
					extra := ""
					if messages {
						path = "/v1/messages"
						extra = `,"max_tokens":64`
					}
					req, _ := http.NewRequest("POST", i.server.URL+path, strings.NewReader(fmt.Sprintf(`{"model":"auto","messages":[{"role":"user","content":"hello"}],"stream":%t%s}`, stream, extra)))
					req.Header.Set("Authorization", "Bearer inference-secret")
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("anthropic-version", "2023-06-01")
					res, e := i.server.Client().Do(req)
					if e != nil {
						t.Fatal(e)
					}
					defer res.Body.Close()
					body, _ := io.ReadAll(res.Body)
					status := res.StatusCode

					if status != 200 || !strings.Contains(string(body), `"model":"b"`) || strings.Contains(string(body), `"model":"a"`) || calls.Load() != 2 || i.calls.Load() != 2 {
						t.Fatalf("status=%d body=%s gen=%d decision=%d", status, body, calls.Load(), i.calls.Load())
					}
					records := i.call("records.list", map[string]any{}, 0, "")
					if !records.OK || !strings.Contains(string(records.Data), `"index":2`) || strings.Contains(string(records.Data), "busy") {
						t.Fatalf("records=%s", records.Data)
					}
					s := i.call("status.get", map[string]any{}, 0, "")
					if !strings.Contains(string(s.Data), `"auto_fallback"`) {
						t.Fatal(string(s.Data))
					}
				})
			}
		}

	}
}
