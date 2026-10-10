package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func nativeFrame(typ, body string) string { return "event: " + typ + "\ndata: " + body + "\n\n" }
func nativeStart() string {
	return nativeFrame("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":0}}}`)
}
func nativeText() string {
	return nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`)
}
func nativeEnd() string {
	return nativeFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`) + nativeFrame("message_stop", `{"type":"message_stop"}`)
}
func collectNative(t *testing.T, body string) ([]routing.Event, error) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	defer s.Close()
	r := TestRequest()
	r.Stream = true
	stream, e := generator(t).Stream(context.Background(), nativeTarget(s.URL), r)
	if e != nil {
		return nil, e
	}
	defer stream.Close()
	var events []routing.Event
	for {
		v, e := stream.Next(context.Background())
		if e == io.EOF {
			return events, nil
		}
		if e != nil {
			return events, e
		}
		events = append(events, v)
	}
}
func TestAnthropicUpstreamReaderBoundary(t *testing.T) {
	valid := nativeStart() + nativeText() + nativeEnd()
	for name, body := range map[string]string{
		"malformed_then_stop": nativeStart() + nativeFrame("content_block_delta", `{bad`) + nativeText() + nativeEnd(),
		"error_after_stop":    valid + nativeFrame("error", `{"type":"error","error":{"type":"overloaded_error","message":"private"}}`),
		"unknown":             nativeStart() + nativeFrame("future", `{"type":"future"}`) + nativeText() + nativeEnd(),
		"mismatch":            nativeStart() + nativeFrame("ping", `{"type":"message_stop"}`) + nativeText() + nativeEnd(),
		"partial":             valid + "data: {",
		"no_stop":             nativeStart() + nativeText(),
		"empty_event":         nativeStart() + "event: ping\n\n" + nativeText() + nativeEnd(),
		"oversize":            nativeStart() + nativeFrame("ping", `{"type":"ping","ignored":"`+strings.Repeat("x", 1024*1024)+`"}`) + nativeText() + nativeEnd(),
	} {
		t.Run(name, func(t *testing.T) {
			events, e := collectNative(t, body)
			if e == nil {
				t.Fatal("invalid stream succeeded")
			}
			for _, v := range events {
				for _, c := range v.Choices {
					if c.FinishReason != nil {
						t.Fatal("failure had successful terminal")
					}
				}
			}
		})
	}
}
func TestAnthropicUpstreamStreamUsageAndCancel(t *testing.T) {
	events, e := collectNative(t, nativeStart()+nativeText()+nativeEnd()+": trailing comment\n\n")
	if e != nil {
		t.Fatal(e)
	}
	finish := 0
	text := ""
	var usage *routing.Usage
	for _, v := range events {
		if v.Usage != nil {
			usage = v.Usage
		}
		for _, c := range v.Choices {
			if c.FinishReason != nil {
				finish++
			}
			if c.Delta != nil {
				text += strings.Trim(string(c.Delta.Content), `"`)
			}
		}
	}
	if finish != 1 || text != "hello" || usage == nil || usage.InputTokens != 60 || usage.OutputTokens != 7 {
		t.Fatalf("finish %d text %q usage %+v", finish, text, usage)
	}
}

func TestAnthropicUpstreamStreamIntegrity(t *testing.T) {
	tool := nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"lookup","input":{}}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`)
	toolEnd := strings.Replace(nativeEnd(), "end_turn", "tool_use", 1)
	thinking := nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason"}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`)
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"tool", tool + toolEnd, true},
		{"parallel", tool + strings.ReplaceAll(strings.ReplaceAll(tool, `"index":0`, `"index":1`), "c1", "c2") + toolEnd, true},
		{"thinking_text", thinking + strings.ReplaceAll(nativeText(), `"index":0`, `"index":1`) + nativeEnd(), true},
		{"text_thinking", nativeText() + strings.ReplaceAll(thinking, `"index":0`, `"index":1`) + nativeEnd(), true},
		{"duplicate_id", tool + strings.ReplaceAll(tool, `"index":0`, `"index":1`) + toolEnd, false},
		{"bad_index", strings.ReplaceAll(nativeText(), `"index":0`, `"index":1`) + nativeEnd(), false},
		{"broken_args", strings.Replace(tool, `\"x\"}`, `\"x\"`, 1) + toolEnd, false},
		{"thinking_tool", thinking + strings.ReplaceAll(tool, `"index":0`, `"index":1`) + toolEnd, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, e := collectNative(t, nativeStart()+tc.body)
			if (e == nil) != tc.ok {
				t.Fatalf("ok=%v error=%v", tc.ok, e)
			}
			if !tc.ok {
				for _, v := range events {
					for _, c := range v.Choices {
						if c.FinishReason != nil {
							t.Fatal("early terminal")
						}
					}
				}
			}
		})
	}
}
func TestAnthropicUpstreamCancelAfterStop(t *testing.T) {
	for _, afterStop := range []bool{false, true} {
		t.Run(fmt.Sprint(afterStop), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, nativeStart()+nativeText())
				if afterStop {
					fmt.Fprint(w, nativeEnd())
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			g := generator(t).(*bifrostGenerator)
			r := TestRequest()
			r.Stream = true
			stream, e := g.Stream(ctx, nativeTarget(s.URL), r)
			if e != nil {
				t.Fatal(e)
			}
			defer stream.Close()
			for {
				event, err := stream.Next(ctx)
				for _, c := range event.Choices {
					if c.FinishReason != nil {
						t.Fatal("timeout succeeded")
					}
				}
				if err != nil {
					if err == io.EOF {
						t.Fatal("timeout became EOF")
					}
					break
				}
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			for _, c := range g.clients {
				if c.active != 0 {
					t.Fatal("connection lease retained")
				}
			}
		})
	}
}

func TestAnthropicUpstreamReaderIdleTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	bc := schemas.NewBifrostContext(context.Background(), time.Time{})
	defer bc.Cancel()
	timed, stop := providerUtils.NewIdleTimeoutReader(pr, pr, 30*time.Millisecond, bc)
	defer stop()
	reader := newAnthropicUpstreamReader(timed, &anthropicUpstreamValidation{})
	go func() { io.WriteString(pw, nativeStart()+nativeText()+nativeEnd()) }()
	for {
		_, _, e := reader.ReadEvent()
		if e != nil {
			var up *routing.UpstreamError
			if !errors.As(e, &up) || up.Status != 504 {
				t.Fatalf("idle error %v", e)
			}
			break
		}
	}
}

func TestAnthropicUpstreamCompressedStream(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			var compressed bytes.Buffer
			zip := gzip.NewWriter(&compressed)
			fmt.Fprint(zip, nativeStart()+nativeText()+nativeEnd())
			if err := zip.Close(); err != nil {
				t.Fatal(err)
			}
			body := compressed.Bytes()
			if corrupt {
				body[len(body)-1] ^= 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Content-Encoding", "gzip")
				w.Write(body)
			}))
			defer server.Close()
			r := TestRequest()
			r.Stream = true
			stream, err := generator(t).Stream(context.Background(), nativeTarget(server.URL), r)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			terminals := 0
			for {
				event, err := stream.Next(context.Background())
				for _, c := range event.Choices {
					if c.FinishReason != nil {
						terminals++
					}
				}
				if err == nil {
					continue
				}
				if corrupt {
					if err == io.EOF || terminals != 0 {
						t.Fatal("corrupt gzip accepted")
					}
				} else if err != io.EOF || terminals != 1 {
					t.Fatalf("terminals=%d error=%v", terminals, err)
				}
				break
			}
		})
	}
}
