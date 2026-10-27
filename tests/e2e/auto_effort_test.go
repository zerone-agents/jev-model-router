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

func TestAutoEffortAgentWire(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, native := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/native=%v/stream=%v", protocol, native, stream), func(t *testing.T) {
					i := newInstance(t)
					i.upstream.Close()
					var decisions, generations atomic.Int32
					i.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var b map[string]json.RawMessage
						if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
							t.Error(err)
						}
						if r.URL.Path == "/v1/systemone" {
							decisions.Add(1)
							var q map[string]any
							json.Unmarshal(b["questions"], &q)
							if len(q) != 2 {
								t.Error("expected two independent questions")
							}
							fmt.Fprint(w, `{"answers":{"model":{"type":"choice","choice":"m0"},"effort":{"type":"choice","choice":"xhigh"}}}`)
							return
						}
						generations.Add(1)
						effort := b["reasoning_effort"]
						if protocol == "anthropic" {
							var out map[string]json.RawMessage
							json.Unmarshal(b["output_config"], &out)
							effort = out["effort"]
						}
						if string(effort) != `"high"` {
							t.Errorf("wrong wire effort %s", effort)
						}
						if protocol == "openai" {
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
								fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
							} else {
								fmt.Fprint(w, `{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
							}
						} else {
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
								fmt.Fprint(w, "event: message_start\ndata: "+`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`+"\n\nevent: content_block_start\ndata: "+`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\nevent: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`+"\n\nevent: content_block_stop\ndata: "+`{"type":"content_block_stop","index":0}`+"\n\nevent: message_delta\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`+"\n\nevent: message_stop\ndata: "+`{"type":"message_stop"}`+"\n\n")
							} else {
								fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
							}
						}
					}))
					i.put("providers.put", map[string]any{"id": "p", "base_url": i.upstream.URL + "/v1", "secret_ref": "env:TEST_PROVIDER_KEY", "protocol": protocol})
					i.put("decision.put", map[string]any{"base_url": i.upstream.URL, "model": "jev", "secret_ref": "env:TEST_PROVIDER_KEY"})
					for _, id := range []string{"a", "b"} {
						m := model(id, true)
						m["auto_effort"] = map[string]any{"allowed": []string{"low", "high"}}
						if protocol == "anthropic" {
							m["upstream_name"] = "claude-test"
						}
						i.put("models.put", m)
					}
					inspect := i.call("route.inspect", map[string]any{"model": "auto", "messages": []map[string]string{{"role": "user", "content": "hello"}}}, 0, "")
					if !inspect.OK || !strings.Contains(string(inspect.Data), `"effective_effort":"high"`) {
						t.Fatalf("inspect: %+v", inspect)
					}
					path := "/v1/chat/completions"
					body := fmt.Sprintf(`{"model":"auto","stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream)
					if native {
						path = "/v1/messages"
						body = fmt.Sprintf(`{"model":"auto","max_tokens":2048,"stream":%t,"messages":[{"role":"user","content":"hello"}]}`, stream)
					}
					req, _ := http.NewRequest("POST", i.server.URL+path, strings.NewReader(body))
					req.Header.Set("Authorization", "Bearer inference-secret")
					req.Header.Set("Content-Type", "application/json")
					if native {
						req.Header.Set("anthropic-version", "2023-06-01")
					}
					resp, err := i.server.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					out, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					status := resp.StatusCode
					if status != 200 || strings.Contains(string(out), `"type":"error"`) || strings.Contains(string(out), `"error":`) {
						t.Fatalf("%d %s", status, out)
					}
					if decisions.Load() != 2 || generations.Load() != 1 {
						t.Fatalf("decisions=%d generation=%d", decisions.Load(), generations.Load())
					}
				})
			}
		}
	}
}
