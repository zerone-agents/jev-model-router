package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func messagesNativeEvent(typ string, fields string) string {
	return "event: " + typ + "\ndata: {\"type\":" + fmt.Sprintf("%q", typ) + fields + "}\n\n"
}
func messagesNativeStart() string {
	return messagesNativeEvent("message_start", `,"message":{"id":"m","type":"message","role":"assistant","model":"up","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"cache_read_input_tokens":10}}`)
}
func messagesNativeEnd(stop string) string {
	return messagesNativeEvent("message_delta", `,"delta":{"stop_reason":`+fmt.Sprintf("%q", stop)+`,"stop_sequence":null},"usage":{"output_tokens":4}`) + messagesNativeEvent("message_stop", "")
}
func messagesNativeTool(index int, id, args string) string {
	arg, _ := json.Marshal(args)
	return messagesNativeEvent("content_block_start", fmt.Sprintf(`,"index":%d,"content_block":{"type":"tool_use","id":%q,"name":"f","input":{}}`, index, id)) + messagesNativeEvent("content_block_delta", fmt.Sprintf(`,"index":%d,"delta":{"type":"input_json_delta","partial_json":%s}`, index, arg)) + messagesNativeEvent("content_block_stop", fmt.Sprintf(`,"index":%d`, index))
}
func messagesNativeText(index int) string {
	return messagesNativeEvent("content_block_start", fmt.Sprintf(`,"index":%d,"content_block":{"type":"text","text":"hello"}`, index)) + messagesNativeEvent("content_block_stop", fmt.Sprintf(`,"index":%d`, index))
}
func readNativeFixture(t *testing.T, body string) ([]string, error) {
	t.Helper()
	p, _ := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`))
	reader := newNativeMessagesEventReader(strings.NewReader(body), nativeMessagesTarget(), p)
	var events []string
	for {
		typ, _, err := reader.ReadEvent()
		if err == io.EOF {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		events = append(events, typ)
	}
}
func TestNativeMessagesStreamToolTruncation(t *testing.T) {
	for _, args := range []string{``, `{`, `{"a":`, `{"a":"x\`, `{"a":"\u12`, `{"a":-`, `{"a":1.`, `{"a":1e+`, `{"a":[{"b":tru`, `{}`, `{"a":9007199254740993}`, `[]`, `{"a":01`, `{"a":"\q`, `{"a":1,}`, `{"a":1,"a":2}`} {
		for _, stop := range []string{"max_tokens", "tool_use", "end_turn"} {
			events, err := readNativeFixture(t, messagesNativeStart()+messagesNativeTool(0, "t", args)+messagesNativeEnd(stop))
			kind := classifyObjectPrefix([]byte(args))
			want := kind == objectPrefixComplete || kind == objectPrefixIncomplete && stop == "max_tokens"
			if (err == nil) != want {
				t.Fatalf("%q %s: %v", args, stop, err)
			}
			if err != nil && strings.Contains(strings.Join(events, ","), "message_stop") {
				t.Fatal("false success")
			}
		}
	}
	for _, tail := range []string{messagesNativeText(1), messagesNativeTool(1, "other", `{}`)} {
		_, err := readNativeFixture(t, messagesNativeStart()+messagesNativeTool(0, "t", `{"a":`)+tail+messagesNativeEnd("max_tokens"))
		if err == nil {
			t.Fatal("earlier incomplete tool accepted")
		}
	}
}
func TestNativeMessagesStreamFidelity(t *testing.T) {
	body := messagesNativeStart()
	index := 0
	for _, kind := range []string{"thinking", "text", "tool", "thinking", "tool", "redacted"} {
		switch kind {
		case "thinking":
			body += messagesNativeEvent("content_block_start", fmt.Sprintf(`,"index":%d,"content_block":{"type":"thinking","thinking":""}`, index)) + messagesNativeEvent("content_block_delta", fmt.Sprintf(`,"index":%d,"delta":{"type":"thinking_delta","thinking":"reason"}`, index)) + messagesNativeEvent("content_block_delta", fmt.Sprintf(`,"index":%d,"delta":{"type":"signature_delta","signature":"signed"}`, index)) + messagesNativeEvent("content_block_stop", fmt.Sprintf(`,"index":%d`, index))
		case "text":
			body += messagesNativeText(index)
		case "tool":
			body += messagesNativeTool(index, fmt.Sprint(index), `{"n":9007199254740993}`)
		case "redacted":
			body += messagesNativeEvent("content_block_start", fmt.Sprintf(`,"index":%d,"content_block":{"type":"redacted_thinking","data":"opaque"}`, index)) + messagesNativeEvent("content_block_stop", fmt.Sprintf(`,"index":%d`, index))
		}
		index++
	}
	body += messagesNativeEnd("tool_use")
	events, err := readNativeFixture(t, body)
	if err != nil || events[len(events)-1] != "message_stop" {
		t.Fatal(events, err)
	}
}
func TestNativeMessagesStreamFailureBoundaries(t *testing.T) {
	valid := messagesNativeStart() + messagesNativeText(0) + messagesNativeEnd("end_turn")
	for _, body := range []string{valid + messagesNativeEvent("error", `,"error":{"type":"invalid_request_error"}`), valid + "junk", strings.TrimSuffix(valid, messagesNativeEvent("message_stop", "")), messagesNativeStart() + messagesNativeEvent("future", ""), messagesNativeStart() + messagesNativeTool(0, "t", `{}`) + messagesNativeTool(1, "t", `{}`) + messagesNativeEnd("tool_use"), messagesNativeStart() + messagesNativeText(1) + messagesNativeEnd("end_turn"), messagesNativeStart() + messagesNativeEvent("content_block_start", `,"index":0,"content_block":{"type":"text","text":"x"}`) + messagesNativeEnd("end_turn"), messagesNativeStart() + messagesNativeEvent("ping", `,"x":"`+strings.Repeat("a", nativeFrameLimit)+`"`)} {
		events, err := readNativeFixture(t, body)
		if err == nil {
			t.Fatal("malformed stream accepted")
		}
		for _, e := range events {
			if e == "message_stop" {
				t.Fatal("false terminal")
			}
		}
	}
}

func TestNativeMessagesStreamWire(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		for _, broken := range []bool{false, true} {
			t.Run(fmt.Sprintf("gzip=%t/broken=%t", compressed, broken), func(t *testing.T) {
				body := messagesNativeStart() + messagesNativeTool(0, "c", `{"a":`) + messagesNativeEnd("max_tokens")
				wire := []byte(body)
				if compressed {
					var b bytes.Buffer
					z := gzip.NewWriter(&b)
					_, _ = z.Write(wire)
					_ = z.Close()
					wire = b.Bytes()
				}
				if broken {
					wire = wire[:len(wire)-4]
				}
				var calls atomic.Int32
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					if compressed {
						w.Header().Set("Content-Encoding", "gzip")
					}
					for _, b := range wire {
						_, _ = w.Write([]byte{b})
						w.(http.Flusher).Flush()
					}
				}))
				defer s.Close()
				p, _ := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
				g := generator(t).(*bifrostGenerator)
				stream, err := g.StreamMessages(context.Background(), nativeTarget(s.URL+"/v1"), p)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				terminal := false
				seen := 0
				for {
					e, err := stream.Next(context.Background())
					if err == io.EOF {
						break
					}
					if err != nil {
						if !broken {
							t.Fatal(err)
						}
						break
					}
					seen++
					terminal = terminal || e.Terminal
					if e.Type == "message_start" && !bytes.Contains(e.Data, []byte(`"model":"fast"`)) {
						t.Fatal("wrong model")
					}
					if e.Type == "message_delta" && !bytes.Contains(e.Data, []byte(`"stop_reason":"max_tokens"`)) {
						t.Fatal("lost stop")
					}
				}
				if terminal == broken || seen == 0 || calls.Load() != 1 {
					t.Fatalf("terminal %t calls %d", terminal, calls.Load())
				}
			})
		}
	}
}
func TestNativeMessagesStreamLimits(t *testing.T) {
	valid := messagesNativeStart() + messagesNativeText(0) + messagesNativeEnd("end_turn")
	ping := messagesNativeEvent("ping", "")
	for _, frames := range []int{maxMessagesStreamFrames, maxMessagesStreamFrames + 1} {
		_, err := readNativeFixture(t, strings.Repeat(ping, frames-5)+valid)
		if (err == nil) != (frames == maxMessagesStreamFrames) {
			t.Fatalf("frames %d: %v", frames, err)
		}
	}
	// Frame size includes SSE delimiters; padding is an ignorable comment.
	for _, size := range []int{nativeFrameLimit, nativeFrameLimit + 1} {
		frame := ":" + strings.Repeat("x", size-len(ping)-2) + "\n" + ping
		_, err := readNativeFixture(t, frame+valid)
		if (err == nil) != (size == nativeFrameLimit) {
			t.Fatalf("frame %d: %v", size, err)
		}
	}
	for _, size := range []int{nativeStreamLimit, nativeStreamLimit + 1} {
		remaining := size - len(valid)
		var pad strings.Builder
		for remaining > 0 {
			n := remaining
			if n > 1000 {
				n = 1000
			}
			if n == 1 {
				pad.WriteByte('\n')
			} else {
				pad.WriteString(":" + strings.Repeat("x", n-2) + "\n")
			}
			remaining -= n
		}
		_, err := readNativeFixture(t, valid+pad.String())
		if (err == nil) != (size == nativeStreamLimit) {
			t.Fatalf("total %d: %v", size, err)
		}
	}
}
func TestNativeMessagesStreamCancelAndErrors(t *testing.T) {
	for typ, status := range map[string]int{"invalid_request_error": 400, "authentication_error": 502, "permission_error": 502, "not_found_error": 404, "request_too_large": 413, "rate_limit_error": 429, "api_error": 500, "timeout_error": 504, "overloaded_error": 529, "unknown": 502} {
		for _, prefix := range []string{"", messagesNativeStart() + messagesNativeText(0)} {
			_, err := readNativeFixture(t, prefix+messagesNativeEvent("error", `,"error":{"type":`+fmt.Sprintf("%q", typ)+`,"message":"SECRET"}`))
			var e *routing.UpstreamError
			if !errors.As(err, &e) || e.Status != status || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("%s: %v", typ, err)
			}
		}
	}
	for _, deadline := range []bool{false, true} {
		for i := 0; i < 20; i++ {
			bc := schemas.NewBifrostContext(context.Background(), time.Time{})
			released := 0
			s := &nativeMessagesStream{ctx: bc, release: func() { released++ }, transport: strings.NewReader(messagesNativeStart())}
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			_, err := s.Next(ctx)
			var e *routing.UpstreamError
			want := 499
			if deadline {
				want = 504
			}
			if !errors.As(err, &e) || e.Status != want {
				t.Fatal(err)
			}
			_ = s.Close()
			_ = s.Close()
			if released != 1 {
				t.Fatal("lease release", released)
			}
		}
	}
}
