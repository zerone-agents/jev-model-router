package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func messagesHandler(t *testing.T, upstream string, limits Limits) http.Handler {
	t.Helper()
	g, err := provider.New(func(string) ([]byte, error) { return []byte("upstream-key"), nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.(io.Closer).Close() })
	s := snapshot()
	s.Providers[0].BaseURL = upstream
	s.Models[0].UpstreamName = "gpt-5"
	s.Models[0].Capabilities.Tools = true
	s.Models[0].Capabilities.Images = true
	store := testStore{s}
	auth := func(h string) (management.Principal, error) {
		if h == "Bearer router-key" {
			return management.Principal{ID: "inference", Role: "inference"}, nil
		}
		if h == "Bearer settings-key" {
			return management.Principal{ID: "settings", Role: "settings"}, nil
		}
		return management.Principal{}, routing.Fail("unauthorized", "invalid credentials")
	}
	return NewHandler(management.New(store, nil), store, &routing.Planner{PrepareCheck: provider.PrepareCheck}, &routing.Executor{Generator: g}, auth, limits)
}
func messagesCall(h http.Handler, body string, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", key)
	r.Header.Set("anthropic-version", "2023-06-01")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestMessagesThroughRouter(t *testing.T) {
	for _, model := range []string{"auto", "external"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", model, stream), func(t *testing.T) {
				var calls atomic.Int32
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["reasoning_effort"] != "max" || body["max_completion_tokens"] != float64(1024) || r.Header.Get("Authorization") != "Bearer upstream-key" {
						t.Errorf("wire: %#v", body)
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\ndata: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"x\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
					} else {
						fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"thought"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
					}
				}))
				defer s.Close()
				w := messagesCall(messagesHandler(t, s.URL, Limits{}), fmt.Sprintf(`{"model":%q,"max_tokens":1024,"stream":%t,"messages":[{"role":"user","content":"query"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`, model, stream), "router-key")
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"external"`) || !strings.Contains(w.Body.String(), "thought") || strings.Contains(w.Body.String(), "extra_fields") || calls.Load() != 1 {
					t.Fatalf("%d %s calls=%d", w.Code, w.Body.String(), calls.Load())
				}
				if w.Header().Get("request-id") == "" || w.Header().Get("request-id") != w.Header().Get("X-Request-ID") {
					t.Fatal("request id missing")
				}
				if stream && (!strings.Contains(w.Body.String(), "event: message_stop") || strings.Contains(w.Body.String(), "[DONE]")) {
					t.Fatal(w.Body.String())
				}
			})
		}
	}
}
func TestMessagesAuthErrors(t *testing.T) {
	h := messagesHandler(t, "http://example.invalid", Limits{})
	for _, tc := range []struct {
		key, authorization string
		status             int
		typ                string
	}{{"", "", 401, "authentication_error"}, {"bad", "", 401, "authentication_error"}, {"settings-key", "", 403, "permission_error"}, {"router-key", "Bearer bad", 401, "authentication_error"}, {"router-key", "Bearer settings-key", 401, "authentication_error"}} {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
		r.Header.Set("x-api-key", tc.key)
		if tc.authorization != "" {
			r.Header.Set("Authorization", tc.authorization)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"type":"error"`) || !strings.Contains(w.Body.String(), tc.typ) || !strings.Contains(w.Body.String(), `"request_id":"`+w.Header().Get("request-id")+`"`) {
			t.Fatalf("%+v -> %d %s", tc, w.Code, w.Body.String())
		}
	}
}
func TestMessagesErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		typ    string
	}{{routing.Fail("no_candidates", "no model"), 422, "invalid_request_error"}, {routing.Fail("timeout", "timed out"), 504, "timeout_error"}, {routing.Fail("config_missing", "missing"), 503, "api_error"}, {&routing.UpstreamError{Status: 401, Body: map[string]any{"message": "SECRET"}}, 502, "api_error"}, {&routing.UpstreamError{Status: 429, Body: map[string]any{"message": "SECRET"}}, 429, "rate_limit_error"}, {&routing.UpstreamError{Status: 529, Body: map[string]any{"message": "SECRET"}}, 529, "overloaded_error"}} {
		w := httptest.NewRecorder()
		writeMessagesError(w, tc.err, "req1")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.typ) || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
func TestMessagesStreamFailure(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if before {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"error":{"message":"password=SECRET"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
			}))
			defer s.Close()
			w := messagesCall(messagesHandler(t, s.URL, Limits{}), `{"model":"auto","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`, "router-key")
			if strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "event: message_stop") {
				t.Fatal(w.Body.String())
			}
			if before {
				if w.Code != 503 || !strings.Contains(w.Body.String(), `"type":"error"`) {
					t.Fatal(w.Code, w.Body.String())
				}
			} else if w.Code != 200 || !strings.Contains(w.Body.String(), "event: error") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestMessagesTimeoutAndCancel(t *testing.T) {
	for _, phase := range []string{"json", "first", "idle", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "idle" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"partial thought\"}}]}\n\n")
					w.(http.Flusher).Flush()
				}
				time.Sleep(80 * time.Millisecond)
			}))
			defer s.Close()
			limits := Limits{FirstEventTimeout: 20 * time.Millisecond, IdleTimeout: 20 * time.Millisecond}
			h := messagesHandler(t, s.URL, limits)
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":"auto","max_tokens":1024,"stream":%t,"messages":[{"role":"user","content":"query"}]}`, phase != "json")))
			r.Header.Set("x-api-key", "router-key")
			r.Header.Set("anthropic-version", "2023-06-01")
			r.Header.Set("Content-Type", "application/json")
			if phase == "cancel" {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				r = r.WithContext(ctx)
				time.AfterFunc(5*time.Millisecond, cancel)
			}
			w := httptest.NewRecorder()
			start := time.Now()
			h.ServeHTTP(w, r)
			if time.Since(start) > 70*time.Millisecond {
				t.Error("request did not stop promptly")
			}
			if phase == "idle" {
				if w.Code != 200 || !strings.Contains(w.Body.String(), "event: error") || !strings.Contains(w.Body.String(), "timeout_error") {
					t.Fatal(w.Code, w.Body.String())
				}
			} else if phase == "cancel" {
				if w.Code != 499 {
					t.Fatal(w.Code, w.Body.String())
				}
			} else if w.Code != 504 {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "event: message_stop") {
				t.Fatal("timeout ended successfully")
			}
		})
	}
}

func TestMessagesValidationDoesNotGenerate(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer s.Close()
	h := messagesHandler(t, s.URL, Limits{MaxBodyBytes: 1024})
	valid := `{"model":"auto","max_tokens":1024,"messages":[{"role":"user","content":"query"}]}`
	for _, body := range []string{`{}`, strings.Replace(valid, `"max_tokens":1024`, `"max_tokens":null`, 1), strings.Replace(valid, `"query"`, `"`+strings.Repeat("x", 1100)+`"`, 1), strings.TrimSuffix(valid, "}") + `,"thinking":{"type":"enabled","budget_tokens":1024}}`} {
		w := messagesCall(h, body, "router-key")
		if w.Code != 400 && w.Code != 413 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, version := range []string{"", "invalid"} {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(valid))
		r.Header.Set("Authorization", "Bearer router-key")
		r.Header.Set("anthropic-version", version)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request sent upstream")
	}
}

func TestMessagesEmptyFramesKeepFirstTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 50 {
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{}}]}\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(time.Millisecond)
		}
	}))
	defer s.Close()
	w := messagesCall(messagesHandler(t, s.URL, Limits{FirstEventTimeout: 10 * time.Millisecond, IdleTimeout: time.Second}), `{"model":"auto","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`, "router-key")
	if w.Code != 504 {
		t.Fatalf("empty upstream frames escaped first timeout: %d %s", w.Code, w.Body.String())
	}
}

func TestMessagesConnectionCapacityMapping(t *testing.T) {
	w := httptest.NewRecorder()
	writeMessagesError(w, routing.ErrGenerationCapacity, "req1")
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"type":"api_error"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMessagesRecordLastOriginalUser(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`)
	}))
	defer s.Close()
	for _, tc := range []struct{ messages, want string }{
		{`[{"role":"user","content":"earlier secret"},{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"tool secret"}]}]`, ""},
		{`[{"role":"user","content":"earlier secret"},{"role":"user","content":"latest text"}]`, "latest text"},
	} {
		sink := &captureRecord{}
		h := messagesHandler(t, s.URL, Limits{Recorder: &routing.Recorder{Sink: sink}})
		w := messagesCall(h, `{"model":"external","max_tokens":1024,"messages":`+tc.messages+`}`, "router-key")
		if w.Code != 200 || sink.record.RequestSummary != tc.want {
			t.Fatalf("%d want %q record=%+v body=%s", w.Code, tc.want, sink.record, w.Body.String())
		}
	}
}

func TestMessagesRefusalStreamFails(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"content":"prefix"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"refusal":"cannot comply"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	w := messagesCall(messagesHandler(t, s.URL, Limits{}), `{"model":"external","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`, "router-key")
	if !strings.Contains(w.Body.String(), `event: error`) || strings.Contains(w.Body.String(), `event: message_stop`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestMessagesResumedThinkingFails(t *testing.T) {
	for _, middle := range []string{
		`{"content":"text"}`,
		`{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
	} {
		t.Run(middle, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"thinking A"}}]}`+"\n\n")
				fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", middle)
				fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"thinking B"}}]}`+"\n\n")
				fmt.Fprint(w, `data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			w := messagesCall(messagesHandler(t, s.URL, Limits{}), `{"model":"external","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`, "router-key")
			body := w.Body.String()
			if !strings.Contains(body, "thinking A") || !strings.Contains(body, "event: error") || strings.Contains(body, "event: message_stop") {
				t.Fatalf("%d %s", w.Code, body)
			}
		})
	}
}
