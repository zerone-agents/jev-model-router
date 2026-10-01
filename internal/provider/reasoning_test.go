package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func reasoningTarget(url string) routing.Target {
	t := target(url)
	// Deliberately use a name which triggers Bifrost's OpenAI effort heuristics.
	t.Model.UpstreamName = "gpt-5"
	for _, thinking := range []*bool{nil, new(true), new(false)} {
		for _, effort := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
			if thinking != nil || effort != "" {
				t.Model.Capabilities.Reasoning = append(t.Model.Capabilities.Reasoning, routing.ReasoningCombination{EnableThinking: thinking, Effort: effort})
			}
		}
	}
	return t
}

func TestReasoningWireRoundTrip(t *testing.T) {
	for _, thinking := range []string{"", `,"chat_template_kwargs":{"enable_thinking":true}`, `,"chat_template_kwargs":{"enable_thinking":false}`} {
		for _, effort := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
			for _, streaming := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", thinking, effort, streaming), func(t *testing.T) {
					extra := thinking
					if effort != "" {
						extra += `,"reasoning_effort":"` + effort + `"`
					}
					r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello","reasoning_content":"previous thought"},{"role":"user","content":"continue"}],"max_completion_tokens":1024`+extra+`}`)
					r.Stream = streaming
					var calls atomic.Int32
					s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
						calls.Add(1)
						var body map[string]json.RawMessage
						if err := json.NewDecoder(q.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						for _, key := range []string{"chat_template_kwargs", "reasoning_effort", "max_completion_tokens"} {
							var got, want any
							if body[key] != nil {
								_ = json.Unmarshal(body[key], &got)
							}
							if r.Options[key] != nil {
								_ = json.Unmarshal(r.Options[key], &want)
							}
							if !reflect.DeepEqual(got, want) {
								t.Errorf("%s changed: got %v want %v", key, got, want)
							}
						}
						var messages []routing.Message
						if err := json.Unmarshal(body["messages"], &messages); err != nil || len(messages) != 3 || messages[1].ReasoningContent == nil || *messages[1].ReasoningContent != "previous thought" {
							t.Errorf("reasoning history changed: %s", body["messages"])
						}
						if body["reasoning"] != nil || body["max_tokens"] != nil || string(body["model"]) != `"gpt-5"` {
							t.Errorf("unexpected fields: %s", body)
						}
						if streaming {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\n")
							fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						} else {
							fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"thought"},"finish_reason":"stop"}]}`)
						}
					}))
					defer s.Close()
					g := generator(t)
					if !streaming {
						out, err := g.Complete(context.Background(), reasoningTarget(s.URL), r)
						if err != nil {
							t.Fatal(err)
						}
						if len(out.Choices) != 1 || out.Choices[0].Message == nil || out.Choices[0].Message.ReasoningContent == nil || *out.Choices[0].Message.ReasoningContent != "thought" || string(out.Choices[0].Message.Content) != `"ok"` || out.Choices[0].FinishReason == nil || *out.Choices[0].FinishReason != "stop" {
							t.Fatalf("invalid completion: %+v", out)
						}
					} else {
						stream, err := g.Stream(context.Background(), reasoningTarget(s.URL), r)
						if err != nil {
							t.Fatal(err)
						}
						defer stream.Close()
						var thought, content string
						finished := false
						for {
							out, err := stream.Next(context.Background())
							if err == io.EOF {
								break
							}
							if err != nil {
								t.Fatal(err)
							}
							for _, choice := range out.Choices {
								if choice.Delta != nil {
									if choice.Delta.ReasoningContent != nil {
										thought += *choice.Delta.ReasoningContent
									}
									var part string
									_ = json.Unmarshal(choice.Delta.Content, &part)
									content += part
								}
								if choice.FinishReason != nil && *choice.FinishReason == "stop" {
									finished = true
								}
							}
						}
						if thought != "thought" || content != "ok" || !finished {
							t.Fatalf("thought=%q content=%q finished=%t", thought, content, finished)
						}
					}
					if calls.Load() != 1 {
						t.Fatalf("calls=%d", calls.Load())
					}
				})
			}
		}
	}
}

func TestUnsupportedReasoningDoesNotGenerate(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	for _, streaming := range []bool{false, true} {
		r := req(t, `{"model":"fast","messages":[{"role":"user","content":"SECRET"}],"reasoning_effort":"high"}`)
		r.Stream = streaming
		var err error
		if streaming {
			_, err = generator(t).Stream(context.Background(), target(s.URL), r)
		} else {
			_, err = generator(t).Complete(context.Background(), target(s.URL), r)
		}
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported reasoning reached upstream")
	}
}

func TestCancelDuringReasoning(t *testing.T) {
	cancelled := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"}],"chat_template_kwargs":{"enable_thinking":true}}`)
	r.Stream = true
	stream, err := generator(t).Stream(ctx, reasoningTarget(s.URL), r)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = stream.Next(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	stream.Close()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not reach upstream")
	}
}
