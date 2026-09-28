package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func target(url string) routing.Target {
	return routing.Target{Provider: routing.Provider{ID: "p", BaseURL: url, SecretRef: "env:KEY"}, Model: routing.Model{ID: "fast", ProviderID: "p", UpstreamName: "gpt-4o-mini", Enabled: true, Location: "cloud", Capabilities: routing.Capabilities{ContextLimit: 8192, Tools: true, Images: true, StructuredOutput: true}}}
}
func req(t *testing.T, body string) routing.Request {
	t.Helper()
	var r routing.Request
	if e := json.Unmarshal([]byte(body), &r); e != nil {
		t.Fatal(e)
	}
	return r
}
func generator(t *testing.T) routing.Generator {
	t.Helper()
	g, e := New(func(string) ([]byte, error) { return []byte("key"), nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { g.(io.Closer).Close() })
	return g
}

const success = `{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`

func TestSelectedTargetOnly(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		fmt.Fprint(w, `{"error":{"message":"SECRET"}}`)
	}))
	defer s.Close()
	_, e := generator(t).Complete(context.Background(), target(s.URL), TestRequest())
	if e == nil || strings.Contains(e.Error(), "SECRET") || calls.Load() != 1 {
		t.Fatalf("error/calls %v %d", e, calls.Load())
	}
}
func TestContentAndOptionsPreserved(t *testing.T) {
	var captured map[string]json.RawMessage
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, success)
	}))
	defer s.Close()
	r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hello"}],"temperature":0,"max_completion_tokens":32}`)
	result, e := generator(t).Complete(context.Background(), target(s.URL), r)
	if e != nil {
		t.Fatal(e)
	}
	if string(captured["temperature"]) != "0" || string(captured["model"]) != `"gpt-4o-mini"` || result.Usage.InputTokens != 4 {
		t.Fatal("payload/usage changed")
	}
}
func TestImageNotFetched(t *testing.T) {
	var hits atomic.Int32
	image := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer image.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, success) }))
	defer s.Close()
	r := req(t, fmt.Sprintf(`{"model":"fast","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":%q}}]}]}`, image.URL))
	if _, e := generator(t).Complete(context.Background(), target(s.URL), r); e != nil {
		t.Fatal(e)
	}
	if hits.Load() != 0 {
		t.Fatal("image fetched")
	}
}
func TestEncryptedExtensionRejected(t *testing.T) {
	r := req(t, `{"model":"fast","messages":[{"role":"assistant","content":"hi","reasoning_details":[{"type":"reasoning.encrypted","data":"secret"}]}]}`)
	if Check(target("https://example.com"), r) == nil {
		t.Fatal("extension accepted")
	}
}
func TestToolFragmentsPreserved(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range []string{`{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}`, `{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}`} {
			fmt.Fprintf(w, "data: {\"id\":\"x\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", delta)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	r := TestRequest()
	r.Stream = true
	stream, e := generator(t).Stream(context.Background(), target(s.URL), r)
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Close()
	var joined string
	for {
		event, e := stream.Next(context.Background())
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range event.Choices {
			if c.Delta != nil {
				for _, tc := range c.Delta.ToolCalls {
					joined += tc.Function.Arguments
				}
			}
		}
	}
	if joined != `{"q":"x"}` {
		t.Fatalf("arguments %q", joined)
	}
}
func TestCancelClosesStream(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	r := TestRequest()
	r.Stream = true
	stream, e := generator(t).Stream(ctx, target(s.URL), r)
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	_, e = stream.Next(ctx)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	start := time.Now()
	stream.Close()
	if time.Since(start) > time.Second {
		t.Fatal("slow cancel")
	}
}
