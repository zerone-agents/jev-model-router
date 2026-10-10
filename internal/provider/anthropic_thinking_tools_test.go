package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestNativeThinkingToolRoundTrip(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, controls := range []string{"", `,"chat_template_kwargs":{"enable_thinking":true}`, `,"reasoning_effort":"high"`} {
			t.Run(fmt.Sprintf("stream=%v/%s", streaming, controls), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if strings.Contains(controls, "enable_thinking") {
						v, _ := body["thinking"].(map[string]any)
						if v["type"] != "adaptive" {
							t.Errorf("thinking lost: %v", body)
						}
					} else if body["thinking"] != nil {
						t.Errorf("invented thinking: %v", body["thinking"])
					}
					if calls == 2 {
						messages := body["messages"].([]any)
						blocks := messages[1].(map[string]any)["content"].([]any)
						b := blocks[0].(map[string]any)
						if b["type"] != "thinking" || b["thinking"] != "Need lookup." || b["signature"] != nil {
							t.Errorf("history changed: %v", b)
						}
						if blocks[1].(map[string]any)["type"] != "tool_use" {
							t.Errorf("tool missing: %v", blocks)
						}
					}
					content := `[{"type":"thinking","thinking":"Need lookup."},{"type":"tool_use","id":"c1","name":"lookup","input":{"q":"x"}}]`
					stop := "tool_use"
					if calls == 2 {
						content = `[{"type":"thinking","thinking":"Have result."},{"type":"text","text":"answer"}]`
						stop = "end_turn"
					}
					if !streaming {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"id":"m","type":"message","role":"assistant","model":"native","content":%s,"stop_reason":"%s","usage":{"input_tokens":10,"output_tokens":7}}`, content, stop)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					reason := "Need lookup."
					if calls == 2 {
						reason = "Have result."
					}
					frames := nativeStart() + nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) + nativeFrame("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":%q}}`, reason)) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`)
					if calls == 1 {
						frames += nativeFrame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c1","name":"lookup","input":{}}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":1}`)
					} else {
						frames += strings.ReplaceAll(strings.ReplaceAll(nativeText(), `"index":0`, `"index":1`), "hello", "answer")
					}
					fmt.Fprint(w, frames+strings.Replace(nativeEnd(), "end_turn", stop, 1))
				}))
				defer server.Close()
				r := req(t, `{"model":"fast","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]`+controls+`}`)
				r.Stream = streaming
				g := generator(t)
				target := nativeTarget(server.URL)
				run := func() routing.Message {
					t.Helper()
					if !streaming {
						out, e := g.Complete(context.Background(), target, r)
						if e != nil {
							t.Fatal(e)
						}
						return *out.Choices[0].Message
					}
					stream, e := g.Stream(context.Background(), target, r)
					if e != nil {
						t.Fatal(e)
					}
					defer stream.Close()
					m := routing.Message{Role: "assistant"}
					reason, text := "", ""
					terminal := 0
					for {
						v, e := stream.Next(context.Background())
						if e == io.EOF {
							break
						}
						if e != nil {
							t.Fatal(e)
						}
						if v.ID == "" {
							t.Fatal("stream response lost message ID")
						}
						for _, c := range v.Choices {
							if c.FinishReason != nil {
								terminal++
							}
							if c.Delta == nil {
								continue
							}
							d := c.Delta
							if d.ReasoningContent != nil {
								reason += *d.ReasoningContent
							}
							if len(d.Content) > 0 {
								var s string
								json.Unmarshal(d.Content, &s)
								text += s
							}
							for _, tc := range d.ToolCalls {
								i := *tc.Index
								for len(m.ToolCalls) <= i {
									m.ToolCalls = append(m.ToolCalls, routing.ToolCall{Type: "function"})
								}
								call := &m.ToolCalls[i]
								if tc.ID != "" {
									call.ID = tc.ID
								}
								if tc.Function.Name != "" {
									call.Function.Name = tc.Function.Name
								}
								call.Function.Arguments += tc.Function.Arguments
							}
						}
					}
					if terminal != 1 {
						t.Fatalf("terminals %d", terminal)
					}
					m.ReasoningContent = &reason
					m.Content, _ = json.Marshal(text)
					return m
				}
				first := run()
				if first.ReasoningContent == nil || *first.ReasoningContent != "Need lookup." || len(first.ToolCalls) != 1 {
					t.Fatalf("first: %+v", first)
				}
				r.Messages = append(r.Messages, first, routing.Message{Role: "tool", ToolCallID: "c1", Content: json.RawMessage(`"value"`)})
				last := run()
				if last.ReasoningContent == nil || *last.ReasoningContent != "Have result." || string(last.Content) != `"answer"` {
					t.Fatalf("last: %+v", last)
				}
				if calls != 2 {
					t.Fatalf("calls %d", calls)
				}
			})
		}
	}
}
