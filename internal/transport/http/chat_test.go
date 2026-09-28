package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testStore struct{ s routing.Snapshot }

func (s testStore) Snapshot(context.Context) (routing.Snapshot, error) { return s.s, nil }
func (s testStore) Apply(context.Context, string, management.Call, func(routing.Snapshot) error) (management.Result, error) {
	panic("write")
}

type testGen struct {
	complete func(context.Context) (routing.Completion, error)
	stream   func(context.Context) (routing.EventStream, error)
}

func (g testGen) Complete(c context.Context, _ routing.Target, _ routing.Request) (routing.Completion, error) {
	return g.complete(c)
}
func (g testGen) Stream(c context.Context, _ routing.Target, _ routing.Request) (routing.EventStream, error) {
	return g.stream(c)
}

type testStream struct {
	next  func(context.Context) (routing.Event, error)
	close func()
}

func (s *testStream) Next(c context.Context) (routing.Event, error) { return s.next(c) }
func (s *testStream) Close() error {
	if s.close != nil {
		s.close()
	}
	return nil
}
func snapshot() routing.Snapshot {
	return routing.Snapshot{Version: 1, Providers: []routing.Provider{{ID: "p"}}, Models: []routing.Model{{ID: "external", ProviderID: "p", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}}}}
}
func handler(g testGen, limits Limits) http.Handler {
	return NewHandler(management.New(testStore{snapshot()}, nil), testStore{snapshot()}, &routing.Planner{}, &routing.Executor{Generator: g}, func(h string) (management.Principal, error) {
		if h == "settings" {
			return management.Principal{Role: "settings"}, nil
		}
		return management.Principal{Role: "inference"}, nil
	}, limits)
}
func event() routing.Event {
	finish := "stop"
	return routing.Event{ID: "upstream", Choices: []routing.Choice{{Delta: &routing.Message{Role: "assistant", Content: json.RawMessage(`"ok"`)}, FinishReason: &finish}}, Usage: &routing.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}
}
func onceStream() *testStream {
	n := 0
	return &testStream{next: func(context.Context) (routing.Event, error) {
		n++
		if n == 1 {
			return event(), nil
		}
		return routing.Event{}, io.EOF
	}}
}
func chat(h http.Handler, stream bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	body := `{"model":"auto","messages":[{"role":"user","content":"hi"}]`
	if stream {
		body += `,"stream":true`
	}
	body += "}"
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	return w
}
func TestModelListHasOneAuto(t *testing.T) {
	w := httptest.NewRecorder()
	handler(testGen{}, Limits{}).ServeHTTP(w, httptest.NewRequest("GET", "/v1/models", nil))
	if w.Code != 200 || strings.Count(w.Body.String(), `"id":"auto"`) != 1 {
		t.Fatal(w.Body.String())
	}
}
func TestJSONAndStreamModelID(t *testing.T) {
	for _, stream := range []bool{false, true} {
		w := chat(handler(testGen{complete: func(context.Context) (routing.Completion, error) { return event(), nil }, stream: func(context.Context) (routing.EventStream, error) { return onceStream(), nil }}, Limits{}), stream)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"external"`) || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
func TestUsageNotMixedWithDecision(t *testing.T) {
	w := httptest.NewRecorder()
	if e := WriteCompletion(w, event(), "external", "req"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(w.Body.String(), `"prompt_tokens":2`) || strings.Contains(w.Body.String(), "decision") {
		t.Fatal(w.Body.String())
	}
}
func TestToolDeltaRoundTrip(t *testing.T) {
	pieces := []string{`{"x":`, `1}`}
	n := 0
	stream := &testStream{next: func(context.Context) (routing.Event, error) {
		if n == len(pieces) {
			return routing.Event{}, io.EOF
		}
		index := 0
		c := routing.Choice{Delta: &routing.Message{ToolCalls: []routing.ToolCall{{Index: &index, Function: routing.CallFunction{Arguments: pieces[n]}}}}}
		n++
		if n == len(pieces) {
			end := "tool_calls"
			c.FinishReason = &end
		}
		return routing.Event{Choices: []routing.Choice{c}}, nil
	}}
	w := httptest.NewRecorder()
	if e := WriteStream(context.Background(), w, stream, "m", "r", time.Second); e != nil {
		t.Fatal(e)
	}
	joined := ""
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var e routing.Event
		json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e)
		for _, c := range e.Choices {
			if c.Delta != nil {
				for _, call := range c.Delta.ToolCalls {
					joined += call.Function.Arguments
				}
			}
		}
	}
	if joined != `{"x":1}` {
		t.Fatal(joined)
	}
}
func TestStreamFailureAfterHeaders(t *testing.T) {
	for _, first := range []bool{true, false} {
		n := 0
		s := &testStream{next: func(context.Context) (routing.Event, error) {
			n++
			if !first && n == 1 {
				return event(), nil
			}
			return routing.Event{}, routing.Fail("upstream_error", "failed")
		}}
		w := chat(handler(testGen{stream: func(context.Context) (routing.EventStream, error) { return s, nil }}, Limits{}), true)
		if !strings.Contains(w.Body.String(), "error") || strings.Contains(w.Body.String(), "[DONE]") {
			t.Fatal(w.Body.String())
		}
	}
}
func TestClientCancel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		entered, done := make(chan struct{}), make(chan struct{})
		g := testGen{complete: func(c context.Context) (routing.Completion, error) {
			close(entered)
			<-c.Done()
			close(done)
			return routing.Completion{}, c.Err()
		}, stream: func(c context.Context) (routing.EventStream, error) {
			close(entered)
			return &testStream{next: func(c context.Context) (routing.Event, error) {
				<-c.Done()
				close(done)
				return routing.Event{}, c.Err()
			}}, nil
		}}
		server := httptest.NewServer(handler(g, Limits{}))
		ctx, cancel := context.WithCancel(context.Background())
		body := `{"model":"auto","messages":[{"role":"user","content":"hi"}],"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `}`
		r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(body))
		returned := make(chan struct{})
		go func() {
			resp, _ := server.Client().Do(r)
			if resp != nil {
				resp.Body.Close()
			}
			close(returned)
		}()
		<-entered
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("upstream not cancelled")
		}
		<-returned
		server.Close()
	}
}
func TestDecisionFirstAndIdleTimeouts(t *testing.T) {
	limits := Limits{DecisionTimeout: 20 * time.Millisecond, FirstEventTimeout: 20 * time.Millisecond, IdleTimeout: 20 * time.Millisecond}
	for _, stage := range []string{"json", "first", "idle"} {
		n := 0
		g := testGen{complete: func(c context.Context) (routing.Completion, error) { <-c.Done(); return routing.Completion{}, c.Err() }, stream: func(context.Context) (routing.EventStream, error) {
			return &testStream{next: func(c context.Context) (routing.Event, error) {
				n++
				if stage == "idle" && n == 1 {
					return event(), nil
				}
				<-c.Done()
				return routing.Event{}, c.Err()
			}}, nil
		}}
		w := chat(handler(g, limits), stage != "json")
		if !strings.Contains(w.Body.String(), "timeout") || strings.Contains(w.Body.String(), "[DONE]") {
			t.Fatalf("%s %s", stage, w.Body.String())
		}
	}
}
func TestSlowClientBoundedBuffer(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	s := &testStream{next: func(context.Context) (routing.Event, error) { calls.Add(1); return event(), nil }}
	b := bufferStream(ctx, s, 2)
	time.Sleep(20 * time.Millisecond)
	if calls.Load() > 3 {
		t.Fatal("unbounded production")
	}
	cancel()
	b.Close()
}

type waitDecider struct{}

func (waitDecider) Choose(c context.Context, _ routing.DecisionConfig, _ routing.DecisionInput) (routing.Decision, error) {
	<-c.Done()
	return routing.Decision{}, c.Err()
}
func TestDecisionDeadline(t *testing.T) {
	s := snapshot()
	other := s.Models[0]
	other.ID = "other"
	s.Models = append(s.Models, other)
	s.Decision.Model = "jev"
	h := NewHandler(management.New(testStore{s}, nil), testStore{s}, &routing.Planner{Decider: waitDecider{}}, &routing.Executor{}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{DecisionTimeout: 10 * time.Millisecond})
	w := chat(h, false)
	if w.Code != 504 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestHealthyStreamOutlivesFirstTimeout(t *testing.T) {
	n := 0
	g := testGen{stream: func(context.Context) (routing.EventStream, error) {
		return &testStream{next: func(c context.Context) (routing.Event, error) {
			n++
			if n > 8 {
				return routing.Event{}, io.EOF
			}
			select {
			case <-c.Done():
				return routing.Event{}, c.Err()
			case <-time.After(5 * time.Millisecond):
				return event(), nil
			}
		}}, nil
	}}
	w := chat(handler(g, Limits{FirstEventTimeout: 15 * time.Millisecond, IdleTimeout: time.Second}), true)
	if !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatal(w.Body.String())
	}
}
func TestEmptyChoicesDoNotResetIdle(t *testing.T) {
	n := 0
	g := testGen{stream: func(context.Context) (routing.EventStream, error) {
		return &testStream{next: func(c context.Context) (routing.Event, error) {
			n++
			if n == 1 {
				return event(), nil
			}
			if n > 30 {
				return routing.Event{}, io.EOF
			}
			select {
			case <-c.Done():
				return routing.Event{}, c.Err()
			case <-time.After(time.Millisecond):
				return routing.Event{Choices: []routing.Choice{{}}}, nil
			}
		}}, nil
	}}
	w := chat(handler(g, Limits{IdleTimeout: 5 * time.Millisecond}), true)
	if !strings.Contains(w.Body.String(), "timeout") {
		t.Fatal(w.Body.String())
	}
}

type failSink struct{}

func (failSink) Append(context.Context, routing.Record) error { return io.ErrClosedPipe }
func TestRecordFailureDoesNotFailGeneration(t *testing.T) {
	rec := &routing.Recorder{Sink: failSink{}, Timeout: time.Millisecond}
	w := chat(handler(testGen{complete: func(context.Context) (routing.Completion, error) { return event(), nil }}, Limits{Recorder: rec}), false)
	if w.Code != 200 || !rec.Degraded() {
		t.Fatal("record error blocked generation or invisible")
	}
}

func TestNoSensitivePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite")
	store, e := state.Open(path, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	rec := &routing.Recorder{Sink: store}
	g := testGen{complete: func(context.Context) (routing.Completion, error) {
		return routing.Completion{}, errors.New("UPSTREAM_SECRET_SENTINEL")
	}}
	s := snapshot()
	s.Models[0].Capabilities.Images = true
	service := management.New(testStore{s}, nil)
	service.RecordsDegraded = rec.Degraded
	h := NewHandler(service, testStore{s}, &routing.Planner{}, &routing.Executor{Generator: g}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{Recorder: rec})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"TEXT_SENTINEL"},{"type":"image_url","image_url":{"url":"https://invalid/IMAGE_SENTINEL"}}]}],"max_completion_tokens":16}`)))
	if strings.Contains(w.Body.String(), "SENTINEL") {
		t.Fatal("error leaked")
	}
	page, e := store.ListRecords(context.Background(), "", 50)
	if e != nil || len(page.Records) != 1 {
		t.Fatal(e, page)
	}
	store.Close()
	b, e := os.ReadFile(path)
	if e != nil || strings.Contains(string(b), "SENTINEL") {
		t.Fatal("sensitive persistence", e)
	}
}
