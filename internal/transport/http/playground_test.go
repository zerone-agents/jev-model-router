package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const pgInput = `{"model":"auto","messages":[{"role":"user","content":"hi"}],"stream":true}`

type pgGenerator struct {
	calls  atomic.Int32
	stream func(context.Context) (routing.EventStream, error)
	t      *testing.T
}

func (g *pgGenerator) Complete(context.Context, routing.Target, routing.Request) (routing.Completion, error) {
	panic("nonstream")
}
func (g *pgGenerator) Stream(ctx context.Context, _ routing.Target, r routing.Request) (routing.EventStream, error) {
	g.calls.Add(1)
	if string(r.Options["max_completion_tokens"]) != "4096" {
		g.t.Error("output cap missing")
	}
	if g.stream != nil {
		return g.stream(ctx)
	}
	return onceStream(), nil
}
func pgFixture(t *testing.T, l playground.Limits, g *pgGenerator) (http.Handler, string, string, *playground.Service) {
	sh, auth := sessionHTTPFixture(t, "")
	token, info := sessionLogin(t, auth, "")
	st, e := state.Open(filepath.Join(t.TempDir(), "quota.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	svc := playground.NewService(st, l, time.Now)
	h := NewPlaygroundHandler(sh, svc, &inference.Service{Store: testStore{snapshot()}, Planner: &routing.Planner{}, DecisionTimeout: time.Second}, &routing.Executor{Generator: g}, l)
	return h, token, info.CSRFToken, svc
}
func TestPlaygroundAdmissionBeforeUpstream(t *testing.T) {
	l := playground.DefaultLimits()
	l.DailyRequests = 1
	g := &pgGenerator{t: t}
	h, token, csrf, _ := pgFixture(t, l, g)
	for _, b := range []string{strings.Replace(pgInput, `"user"`, `"system"`, 1), strings.Replace(pgInput, `"hi"`, `"`+strings.Repeat("中", 11000)+`"`, 1), strings.Replace(pgInput, `"stream":true`, `"stream":true,"max_tokens":9999`, 1), "{"} {
		w := sessionRequest(h, "POST", "/admin/v1/playground/completions", b, token, csrf)
		if w.Code < 400 {
			t.Fatal("accepted invalid", w.Body.String())
		}
	}
	if g.calls.Load() != 0 {
		t.Fatal("invalid input reached upstream")
	}
	w := sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "event: done") || !strings.Contains(w.Body.String(), `"model_id":"external"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "day") {
		t.Fatal(w.Code, w.Body.String())
	}
	if g.calls.Load() != 1 {
		t.Fatal("limited request reached upstream")
	}
}
func TestPlaygroundCancellationAndTimeout(t *testing.T) {
	l := playground.DefaultLimits()
	l.Timeout = 20 * time.Millisecond
	g := &pgGenerator{t: t, stream: func(context.Context) (routing.EventStream, error) {
		return &testStream{next: func(ctx context.Context) (routing.Event, error) { <-ctx.Done(); return routing.Event{}, ctx.Err() }}, nil
	}}
	h, token, csrf, svc := pgFixture(t, l, g)
	w := sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
	if !strings.Contains(w.Body.String(), "timeout") || strings.Contains(w.Body.String(), "event: done") {
		t.Fatal(w.Body.String())
	}
	lease, e := svc.Acquire(context.Background(), "different")
	if e != nil {
		t.Fatal(e)
	}
	lease.Release()
}
func TestPlaygroundReasoningKeptSeparate(t *testing.T) {
	l := playground.DefaultLimits()
	g := &pgGenerator{t: t, stream: func(context.Context) (routing.EventStream, error) {
		s := onceStream()
		next := s.next
		s.next = func(ctx context.Context) (routing.Event, error) {
			v, e := next(ctx)
			if len(v.Choices) > 0 {
				r := "hidden"
				v.Choices[0].Delta.ReasoningContent = &r
				v.Choices[0].Delta.Content = json.RawMessage(`"visible"`)
			}
			return v, e
		}
		return s, nil
	}}
	h, token, csrf, _ := pgFixture(t, l, g)
	w := sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
	if !strings.Contains(w.Body.String(), `"reasoning_content":"hidden"`) || !strings.Contains(w.Body.String(), `"content":"visible"`) {
		t.Fatal(w.Body.String())
	}
}

func TestPlaygroundCancelReleasesSameSession(t *testing.T) {
	l := playground.DefaultLimits()
	g := &pgGenerator{t: t}
	started := make(chan struct{})
	stopped := make(chan struct{})
	g.stream = func(context.Context) (routing.EventStream, error) {
		return &testStream{next: func(ctx context.Context) (routing.Event, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return routing.Event{}, ctx.Err()
		}}, nil
	}
	h, token, csrf, svc := pgFixture(t, l, g)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "http://localhost/admin/v1/playground/completions", strings.NewReader(pgInput)).WithContext(ctx)
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(httptest.NewRecorder(), r) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel stuck")
	}
	<-stopped
	id, e := h.(*playgroundHTTP).sessions.AuthenticatePlayground(r.Clone(context.Background()))
	if e != nil {
		t.Fatal(e)
	}
	lease, e := svc.Acquire(context.Background(), id)
	if e != nil {
		t.Fatal("session slot leaked", e)
	}
	lease.Release()
	q, e := svc.Status(context.Background(), id)
	if e != nil || q.DayRemaining != 198 {
		t.Fatal("cancel refunded quota", q, e)
	}
}
func TestPlaygroundInputBoundaries(t *testing.T) {
	l := playground.DefaultLimits()
	l.InputBytes = 6
	l.BodyBytes = 2048
	l.MaxMessages = 2
	g := &pgGenerator{t: t}
	h, token, csrf, _ := pgFixture(t, l, g)
	for _, tc := range []struct {
		body   string
		status int
	}{{strings.Replace(pgInput, `"hi"`, `"中文"`, 1), 200}, {strings.Replace(pgInput, `"hi"`, `"中文a"`, 1), 413}, {strings.Replace(pgInput, `"hi"`, `"`+strings.Repeat("a", 2048)+`"`, 1), 413}, {`{"model":"auto","stream":true,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b","reasoning_content":"12345"}]}`, 413}, {`{"model":"auto","stream":true,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`, 413}} {
		w := sessionRequest(h, "POST", "/admin/v1/playground/completions", tc.body, token, csrf)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if g.calls.Load() != 1 {
		t.Fatal("invalid input reached upstream")
	}
}

type pgDecider struct{ calls int }

func (d *pgDecider) Choose(_ context.Context, _ routing.DecisionConfig, _ routing.DecisionInput) (routing.Decision, error) {
	d.calls++
	return routing.Decision{ModelID: "external"}, nil
}
func TestPlaygroundSingleDecisionAndRecord(t *testing.T) {
	l := playground.DefaultLimits()
	g := &pgGenerator{t: t}
	h, token, csrf, _ := pgFixture(t, l, g)
	p := h.(*playgroundHTTP)
	cfg := snapshot()
	cfg.Decision = routing.DecisionConfig{BaseURL: "http://decision.invalid", Model: "jev", SecretRef: "env:TEST"}
	second := cfg.Models[0]
	second.ID = "second"
	cfg.Models = append(cfg.Models, second)
	p.inference.Store = testStore{cfg}
	d := &pgDecider{}
	p.inference.Planner.Decider = d
	st, e := state.Open(filepath.Join(t.TempDir(), "records.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	p.inference.Recorder = &routing.Recorder{Sink: st}
	w := sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
	if w.Code != 200 || d.calls != 1 || g.calls.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), d.calls, g.calls.Load())
	}
	page, e := st.ListRecords(context.Background(), "", 50)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(page)
	if strings.Count(string(b), `"request_id"`) != 1 || !strings.Contains(string(b), `"model_id":"external"`) {
		t.Fatal(string(b))
	}
}
func TestPlaygroundFailurePathsRelease(t *testing.T) {
	for _, kind := range []string{"panic", "upstream", "incomplete", "tool"} {
		t.Run(kind, func(t *testing.T) {
			l := playground.DefaultLimits()
			g := &pgGenerator{t: t, stream: func(context.Context) (routing.EventStream, error) {
				if kind == "panic" {
					panic("fixture panic")
				}
				if kind == "upstream" {
					return nil, errors.New("sensitive-provider-text")
				}
				return &testStream{next: func(context.Context) (routing.Event, error) {
					if kind == "tool" {
						return routing.Event{Choices: []routing.Choice{{Delta: &routing.Message{ToolCalls: []routing.ToolCall{{ID: "t"}}}}}}, nil
					}
					return routing.Event{}, io.EOF
				}}, nil
			}}
			h, token, csrf, svc := pgFixture(t, l, g)
			func() {
				defer func() {
					if v := recover(); v != nil && kind != "panic" {
						t.Error(v)
					}
				}()
				w := sessionRequest(h, "POST", "/admin/v1/playground/completions", pgInput, token, csrf)
				if strings.Contains(w.Body.String(), "event: done") || strings.Contains(w.Body.String(), "sensitive-provider-text") {
					t.Error(w.Body.String())
				}
			}()
			r := httptest.NewRequest("GET", "http://localhost/admin/v1/playground", nil)
			r.Header.Set("X-Jev-Session", "1")
			r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
			id, e := h.(*playgroundHTTP).sessions.AuthenticatePlayground(r)
			if e != nil {
				t.Fatal(e)
			}
			lease, e := svc.Acquire(context.Background(), id)
			if e != nil {
				t.Fatal(e)
			}
			lease.Release()
		})
	}
}

func TestPlaygroundFailureRetainsSelectedModel(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprint(late), func(t *testing.T) {
			failure := &routing.UpstreamError{Status: 429, Body: map[string]any{"message": "SECRET", "code": "insufficient_quota"}}
			var w *httptest.ResponseRecorder
			g := &pgGenerator{t: t, stream: func(context.Context) (routing.EventStream, error) {
				// Planning must be visible even while upstream has not responded.
				if w == nil || !strings.Contains(w.Body.String(), `"model_id":"external"`) {
					t.Error("route not flushed before generation")
				}
				if !late {
					return nil, failure
				}
				n := 0
				return &testStream{next: func(context.Context) (routing.Event, error) {
					n++
					if n == 1 {
						return routing.Event{Choices: []routing.Choice{{Index: 0, Delta: &routing.Message{Content: json.RawMessage(`"partial"`)}}}}, nil
					}
					return routing.Event{}, failure
				}}, nil
			}}
			h, token, csrf, _ := pgFixture(t, playground.DefaultLimits(), g)
			// Reuse the session fixture's authenticated request, but retain its recorder
			// before handler invocation so the upstream can assert flush ordering.
			req := httptest.NewRequest("POST", "/admin/v1/playground/completions", strings.NewReader(pgInput))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://localhost")
			req.Host = "localhost"
			req.Header.Set("X-CSRF-Token", csrf)
			req.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			body := w.Body.String()
			if !strings.Contains(body, "upstream_quota") || !strings.Contains(body, `"upstream_status":429`) || !strings.Contains(body, `"stage":"generation"`) || strings.Contains(body, "SECRET") || strings.Count(body, "event: error") != 1 || strings.Contains(body, "event: done") {
				t.Fatal(body)
			}
			if late && !strings.Contains(body, "partial") {
				t.Fatal(body)
			}
		})
	}
}

func TestPublicAndPlaygroundDiagnosticParity(t *testing.T) {
	err := &routing.UpstreamError{Status: 401, Body: map[string]any{"message": "provider supplied", "code": "invalid_api_key"}}
	public := errorBody(err)["error"].(map[string]any)["diagnostic"].(routing.Diagnostic)
	pg := playgroundDiagnostic(err, "request", "generation")
	if public != pg.Diagnostic {
		t.Fatalf("public=%+v playground=%+v", public, pg)
	}
	if _, ok := err.Body["diagnostic"]; ok {
		t.Fatal("mutated upstream error")
	}
}
