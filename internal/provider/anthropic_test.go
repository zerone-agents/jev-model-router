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

func TestPreparedMessagesWire(t *testing.T) {
	body := `{"model":"external","max_tokens":1024,"system":"rules","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":{"type":"url","url":"https://example.invalid/image.png"}}]}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"stop_sequences":["END"],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"tool_choice":{"type":"any","disable_parallel_tool_use":true}}`
	p, err := PrepareMessages(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire map[string]any
		json.NewDecoder(r.Body).Decode(&wire)
		if wire["reasoning_effort"] != "max" || wire["parallel_tool_calls"] != false || wire["max_completion_tokens"] != float64(1024) {
			t.Errorf("parameters changed: %#v", wire)
		}
		if wire["chat_template_kwargs"].(map[string]any)["enable_thinking"] != true {
			t.Error("thinking missing")
		}
		if wire["stop"].([]any)[0] != "END" {
			t.Error("stop missing")
		}
		messages := wire["messages"].([]any)
		if len(messages) != 2 {
			t.Errorf("message grouping: %#v", messages)
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("credential changed")
		}
		fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"thought"},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
	}))
	defer s.Close()
	g := generator(t).(*bifrostGenerator)
	target := reasoningTarget(s.URL)
	target.Model.Capabilities.Tools = true
	target.Model.Capabilities.Images = true
	if err = p.Check(target); err != nil {
		t.Fatal(err)
	}
	out, err := g.CompleteMessages(context.Background(), target, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"thinking":"thought"`) || !strings.Contains(string(out), `"stop_reason":"max_tokens"`) || strings.Contains(string(out), "extra_fields") {
		t.Fatal(string(out))
	}
	if err = p.Check(target); err != nil {
		t.Fatal("prepared mutated", err)
	}
}

func TestPreparedMessagesValidation(t *testing.T) {
	for _, extra := range []string{`,"thinking":{"type":"enabled","budget_tokens":1024}`, `,"cache_control":{"type":"ephemeral"}`, `,"unknown_secret":"DO_NOT_ECHO"`, `,"max_completion_tokens":12`} {
		_, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":4096,"messages":[{"role":"user","content":"secret"}]` + extra + `}`))
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "DO_NOT_ECHO") {
			t.Fatalf("%s: %v", extra, err)
		}
	}
	for _, n := range []int{0, 1, 15, 10000001} {
		_, err := PrepareMessages(json.RawMessage(fmt.Sprintf(`{"model":"auto","max_tokens":%d,"messages":[{"role":"user","content":"query"}]}`, n)))
		if err == nil {
			t.Fatalf("limit %d accepted", n)
		}
	}
}

func TestPreparedMessagesStream(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	p, err := PrepareMessages(json.RawMessage(`{"model":"external","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}],"thinking":{"type":"adaptive"}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := generator(t).(*bifrostGenerator)
	stream, err := g.StreamMessages(context.Background(), target(s.URL), p)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var types []string
	for {
		e, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, e.Type)
		if strings.Contains(string(e.Data), "extra_fields") {
			t.Fatal(string(e.Data))
		}
		if e.Type == "message_start" && strings.Contains(string(e.Data), `"input_tokens":0`) {
			t.Fatal("invented initial usage")
		}
		if e.Type == "message_delta" && !strings.Contains(string(e.Data), `"input_tokens":7`) {
			t.Fatal(string(e.Data))
		}
	}
	if len(types) == 0 || types[0] != "message_start" || types[len(types)-1] != "message_stop" {
		t.Fatal(types)
	}
}

func TestPreparedMessagesToolRound(t *testing.T) {
	p, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":1024,"messages":[{"role":"user","content":"lookup"},{"role":"assistant","content":[{"type":"thinking","thinking":"old thought","signature":""},{"type":"text","text":"checking"},{"type":"tool_use","id":"call1","name":"lookup","input":{"q":"value"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"result"},{"type":"text","text":"continue"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := p.Projection()
	if err = routing.ValidateRequest(r); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != 4 || r.Messages[1].ReasoningContent == nil || len(r.Messages[1].ToolCalls) != 1 || r.Messages[2].ToolCallID != "call1" {
		t.Fatalf("%+v", r.Messages)
	}
}

func TestPreparedMessagesNumbersAndUTF8(t *testing.T) {
	for _, limit := range []string{"1024.0", "1.024e3"} {
		p, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":` + limit + `,"messages":[{"role":"user","content":"query"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if string(p.Projection().Options["max_completion_tokens"]) != "1024" {
			t.Fatal("limit changed")
		}
	}
	_, err := PrepareMessages(append([]byte(`{"model":"auto","max_tokens":1024,"messages":[{"role":"user","content":"`), append([]byte{0xff}, []byte(`"}]}`)...)...))
	if err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}

func TestPreparedMessagesMissingAndZeroUsage(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, reported := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/reported=%t", streaming, reported), func(t *testing.T) {
				u := ""
				if reported {
					u = `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`
				}
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]%s}\n\ndata: [DONE]\n\n", u)
					} else {
						fmt.Fprintf(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]%s}`, u)
					}
				}))
				defer s.Close()
				p, err := PrepareMessages(json.RawMessage(fmt.Sprintf(`{"model":"external","max_tokens":1024,"stream":%t,"messages":[{"role":"user","content":"query"}]}`, streaming)))
				if err != nil {
					t.Fatal(err)
				}
				g := generator(t).(*bifrostGenerator)
				var output string
				if !streaming {
					body, err := g.CompleteMessages(context.Background(), target(s.URL), p)
					if err != nil {
						t.Fatal(err)
					}
					output = string(body)
				} else {
					stream, err := g.StreamMessages(context.Background(), target(s.URL), p)
					if err != nil {
						t.Fatal(err)
					}
					defer stream.Close()
					for {
						event, err := stream.Next(context.Background())
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						if event.Type == "message_delta" {
							output = string(event.Data)
						}
					}
				}
				if strings.Contains(output, `"input_tokens":0`) != reported {
					t.Fatalf("reported=%t: %s", reported, output)
				}
			})
		}
	}
}

func TestPreparedMessagesThinkingOnlyHistory(t *testing.T) {
	p, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":1024,"messages":[{"role":"user","content":"go"},{"role":"assistant","content":[{"type":"thinking","thinking":"THOUGHT","signature":""}]},{"role":"user","content":"continue"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := p.Projection()
	if len(r.Messages) != 3 || r.Messages[1].ReasoningContent == nil || *r.Messages[1].ReasoningContent != "THOUGHT" {
		t.Fatalf("thinking-only turn lost: %+v", r.Messages)
	}
}
func TestPreparedMessagesAssistantGroupOrder(t *testing.T) {
	for _, content := range []string{
		`{"role":"assistant","content":"FIRST"},{"role":"assistant","content":[{"type":"thinking","thinking":"SECOND","signature":""}]}`,
		`{"role":"assistant","content":[{"type":"tool_use","id":"c","name":"lookup","input":{}}]},{"role":"assistant","content":"AFTER"}`,
	} {
		_, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":1024,"messages":[{"role":"user","content":"go"},` + content + `,{"role":"user","content":"continue"}]}`))
		if err == nil {
			t.Fatal("unrepresentable assistant group silently accepted")
		}
	}
}

func TestPreparedMessagesCachedUsage(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			u := `"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":80}}`
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],%s}\n\ndata: [DONE]\n\n", u)
				} else {
					fmt.Fprintf(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],%s}`, u)
				}
			}))
			defer s.Close()
			p, err := PrepareMessages(json.RawMessage(fmt.Sprintf(`{"model":"external","max_tokens":1024,"stream":%t,"messages":[{"role":"user","content":"query"}]}`, streaming)))
			if err != nil {
				t.Fatal(err)
			}
			g := generator(t).(*bifrostGenerator)
			var body json.RawMessage
			if !streaming {
				body, err = g.CompleteMessages(context.Background(), target(s.URL), p)
			} else {
				stream, e := g.StreamMessages(context.Background(), target(s.URL), p)
				if e != nil {
					t.Fatal(e)
				}
				defer stream.Close()
				for {
					event, e := stream.Next(context.Background())
					if e == io.EOF {
						break
					}
					if e != nil {
						t.Fatal(e)
					}
					if event.Type == "message_delta" {
						body = event.Data
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var out struct{ Usage map[string]int64 }
			if err = json.Unmarshal(body, &out); err != nil {
				t.Fatal(err)
			}
			if out.Usage["input_tokens"] != 20 || out.Usage["cache_read_input_tokens"] != 80 || out.Usage["output_tokens"] != 10 {
				t.Fatalf("cache attribution changed: %s", body)
			}
		})
	}
}

func TestPreparedMessagesStopContradiction(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, finish := range []string{"length", "tool_calls"} {
			t.Run(fmt.Sprintf("%t/%s", streaming, finish), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":%q,\"stop\":\"END\"}]}\n\ndata: [DONE]\n\n", finish)
					} else {
						fmt.Fprintf(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"partial"},"finish_reason":%q,"stop":"END"}]}`, finish)
					}
				}))
				defer s.Close()
				p, err := PrepareMessages(json.RawMessage(fmt.Sprintf(`{"model":"external","max_tokens":1024,"stream":%t,"messages":[{"role":"user","content":"query"}]}`, streaming)))
				if err != nil {
					t.Fatal(err)
				}
				g := generator(t).(*bifrostGenerator)
				if !streaming {
					_, err = g.CompleteMessages(context.Background(), target(s.URL), p)
				} else {
					stream, e := g.StreamMessages(context.Background(), target(s.URL), p)
					if e != nil {
						err = e
					} else {
						defer stream.Close()
						for {
							event, e := stream.Next(context.Background())
							if e != nil {
								err = e
								break
							}
							if event.Terminal {
								t.Fatal("contradictory stop completed successfully")
							}
						}
					}
				}
				if err == nil || err == io.EOF {
					t.Fatal("contradictory stop accepted")
				}
			})
		}
	}
}

func TestPreparedMessagesStreamBudget(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 20 {
			fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", strings.Repeat("x", 256))
		}
		fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	p, err := PrepareMessages(json.RawMessage(`{"model":"external","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"query"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := generator(t).(*bifrostGenerator).StreamMessages(context.Background(), target(s.URL), p)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	bounded := stream.(*messagesStream)
	bounded.byteLimit = 1024
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			t.Fatal("oversized response accepted")
		}
		if err != nil {
			break
		}
		if event.Terminal {
			t.Fatal("budget violation emitted success")
		}
	}
	if bounded.state.TextBuffer.Len() > 1024 {
		t.Fatalf("converter accumulated %d bytes", bounded.state.TextBuffer.Len())
	}
}

func TestPreparedMessagesMatchedStop(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\",\"stop\":\"END\"}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop","stop":"END"}]}`)
				}
			}))
			defer s.Close()
			p, err := PrepareMessages(json.RawMessage(fmt.Sprintf(`{"model":"external","max_tokens":1024,"stream":%t,"stop_sequences":["END"],"messages":[{"role":"user","content":"query"}]}`, streaming)))
			if err != nil {
				t.Fatal(err)
			}
			g := generator(t).(*bifrostGenerator)
			var body json.RawMessage
			if !streaming {
				body, err = g.CompleteMessages(context.Background(), target(s.URL), p)
			} else {
				stream, e := g.StreamMessages(context.Background(), target(s.URL), p)
				if e != nil {
					t.Fatal(e)
				}
				defer stream.Close()
				for {
					event, e := stream.Next(context.Background())
					if e == io.EOF {
						break
					}
					if e != nil {
						t.Fatal(e)
					}
					if event.Type == "message_delta" {
						body = event.Data
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), `"stop_sequence":"END"`) || !strings.Contains(string(body), `"stop_reason":"stop_sequence"`) {
				t.Fatal(string(body))
			}
		})
	}
}

func TestMessagesParallelToolsSameFrame(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"first","arguments":"{\"x\":"}},{"index":1,"id":"b","type":"function","function":{"name":"second","arguments":"{\"y\":"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	p, err := PrepareMessages(json.RawMessage(`{"model":"external","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := generator(t).(*bifrostGenerator).StreamMessages(context.Background(), target(s.URL), p)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var output string
	ids := map[int]string{}
	args := map[string]string{}
	for {
		e, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		output += string(e.Data)
		var event struct {
			Index        int `json:"index"`
			ContentBlock struct {
				ID string `json:"id"`
			} `json:"content_block"`
			Delta struct {
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(e.Data, &event); err != nil {
			t.Fatal(err)
		}
		if e.Type == "content_block_start" {
			ids[event.Index] = event.ContentBlock.ID
		}
		if e.Type == "content_block_delta" {
			args[ids[event.Index]] += event.Delta.PartialJSON
		}
	}
	if args["a"] != `{"x":1}` || args["b"] != `{"y":2}` {
		t.Fatalf("tool arguments not paired: %#v", args)
	}
	for _, want := range []string{`"id":"a"`, `"id":"b"`, `"name":"first"`, `"name":"second"`, `message_stop`} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %s: %s", want, output)
		}
	}
}

func TestMessagesInvalidOutput(t *testing.T) {
	for _, message := range []string{
		`{"role":"assistant","content":null,"refusal":"cannot comply"}`,
		`{"role":"assistant","tool_calls":[{"id":"same","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"same","type":"function","function":{"name":"b","arguments":"{}"}}]}`,
	} {
		t.Run(message, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"id":"x","choices":[{"index":0,"message":%s,"finish_reason":"stop"}]}`, message)
			}))
			defer s.Close()
			p, err := PrepareMessages(json.RawMessage(`{"model":"external","max_tokens":1024,"messages":[{"role":"user","content":"query"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if result, err := generator(t).(*bifrostGenerator).CompleteMessages(context.Background(), target(s.URL), p); err == nil {
				t.Fatalf("invalid output accepted: %s", result)
			}
		})
	}
}

func TestMessagesSummaryOriginalLastUser(t *testing.T) {
	for _, tc := range []struct{ messages, want string }{
		{`[{"role":"user","content":"earlier secret"},{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"tool secret"}]}]`, ""},
		{`[{"role":"user","content":"earlier secret"},{"role":"user","content":"latest text"}]`, "latest text"},
	} {
		p, err := PrepareMessages(json.RawMessage(`{"model":"auto","max_tokens":1024,"messages":` + tc.messages + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Summary(); got != tc.want {
			t.Fatalf("want %q got %q", tc.want, got)
		}
	}
}
