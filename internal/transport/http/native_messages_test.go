package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type nativeMessagesDecider struct {
	choose func(routing.DecisionInput) (routing.Decision, error)
}

func (d nativeMessagesDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	return d.choose(in)
}
func nativeMessagesHandler(t *testing.T, url string, decider routing.Decider, mixed bool, limits Limits) http.Handler {
	t.Helper()
	g, err := provider.New(func(string) ([]byte, error) { return []byte("upstream-key"), nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.(io.Closer).Close() })
	s := snapshot()
	s.Providers[0].BaseURL = url + "/v1"
	s.Providers[0].Protocol = routing.ProtocolAnthropic
	s.Models[0].UpstreamName = "claude-test"
	s.Models[0].Capabilities.Tools = true
	s.Models[0].Capabilities.Images = true
	s.Models[0].Capabilities.ContextLimit = 100000
	if mixed {
		p := s.Providers[0]
		p.ID = "chat"
		p.Protocol = routing.ProtocolOpenAI
		s.Providers = append(s.Providers, p)
		m := s.Models[0]
		m.ID = "chat"
		m.ProviderID = "chat"
		s.Models = append(s.Models, m)
		s.Decision.Model = "decision"
	}
	store := testStore{s}
	auth := func(h string) (management.Principal, error) {
		if h == "Bearer router-key" {
			return management.Principal{ID: "user", Role: "inference"}, nil
		}
		if h == "Bearer settings-key" {
			return management.Principal{ID: "settings", Role: "settings"}, nil
		}
		return management.Principal{}, routing.Fail("unauthorized", "invalid credentials")
	}
	return NewHandler(management.New(store, nil), store, &routing.Planner{Decider: decider, PrepareCheck: provider.PrepareCheck}, &routing.Executor{Generator: g}, auth, limits)
}
func TestNativeMessagesHTTPMixedRouting(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/messages" {
			t.Error("wrong upstream")
		}
		fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"up","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer up.Close()
	var choices atomic.Int32
	decider := nativeMessagesDecider{func(in routing.DecisionInput) (routing.Decision, error) {
		choices.Add(1)
		if len(in.Candidates) != 2 {
			t.Error("lost compatible candidate")
		}
		raw, _ := json.Marshal(in.Request)
		if strings.Contains(string(raw), "PRIVATE") {
			t.Error("decision leak")
		}
		return routing.Decision{ModelID: "external"}, nil
	}}
	h := nativeMessagesHandler(t, up.URL, decider, true, Limits{})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			extra := ""
			if i%2 == 0 {
				extra = `,"thinking":{"type":"enabled","budget_tokens":1024}`
			}
			w := messagesCall(h, `{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]`+extra+`}`, "router-key")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"external"`) || w.Header().Get("request-id") != w.Header().Get("X-Request-ID") {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if calls.Load() != 12 || choices.Load() != 6 {
		t.Fatalf("calls %d choices %d", calls.Load(), choices.Load())
	}
	w := messagesCall(h, `{"model":"chat","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hi"}]}`, "router-key")
	if w.Code != 400 || calls.Load() != 12 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestNativeMessagesHTTPErrorContract(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, status := range []int{400, 404, 429, 529} {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, `{"type":"error","error":{"type":"api_error","message":"provider rejected upstream-key","api_key":"PRIVATE"}}`)
			}))
			h := nativeMessagesHandler(t, up.URL, nil, false, Limits{})
			body := fmt.Sprintf(`{"model":"external","max_tokens":2048,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
			w := messagesCall(h, body, "router-key")
			if w.Code != status || !strings.Contains(w.Body.String(), `"type":"error"`) || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "upstream-key") || !strings.Contains(w.Body.String(), "provider rejected") || !strings.Contains(w.Body.String(), "upstream_error") || calls.Load() != 1 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			for key, want := range map[string]int{"bad": 401, "settings-key": 403} {
				w = messagesCall(h, body, key)
				if w.Code != want || !strings.Contains(w.Body.String(), `"request_id"`) {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			up.Close()
		}
	}
}

func TestNativeMessagesHTTPLargeNativeHistory(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) != 2 || len(body.Messages[0].Content) != 600000 || len(body.Messages[1].Content) != 600000 {
			t.Error("native history changed")
		}
		fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"up","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer up.Close()
	h := nativeMessagesHandler(t, up.URL, nativeMessagesDecider{func(routing.DecisionInput) (routing.Decision, error) {
		t.Error("incompatible Chat candidate reached decision")
		return routing.Decision{}, nil
	}}, true, Limits{})
	for _, model := range []string{"auto", "external"} {
		raw, _ := json.Marshal(map[string]any{"model": model, "max_tokens": 2048, "messages": []map[string]string{{"role": "user", "content": strings.Repeat("a", 600000)}, {"role": "user", "content": strings.Repeat("b", 600000)}}})
		w := messagesCall(h, string(raw), "router-key")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatal("calls", calls.Load())
	}
}

func TestNativeMessagesSSEErrorDetails(t *testing.T) {
	for _, started := range []bool{false, true} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if started {
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"up\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
				w.(http.Flusher).Flush()
			}
			fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"provider rejected upstream-key\",\"api_key\":\"PRIVATE\"}}\n\n")
		}))
		h := nativeMessagesHandler(t, up.URL, nil, false, Limits{})
		w := messagesCall(h, `{"model":"external","max_tokens":2048,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, "router-key")
		body := w.Body.String()
		if strings.Contains(body, "upstream-key") || strings.Contains(body, "PRIVATE") || strings.Contains(body, "message_stop") || !strings.Contains(body, "provider rejected") || !strings.Contains(body, `"upstream_status":401`) {
			t.Fatalf("started=%v: %d %s", started, w.Code, body)
		}
		up.Close()
	}
}
