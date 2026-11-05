package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/decision"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

type httpPrivacyClassifier struct {
	calls int
	raw   json.RawMessage
}

func (c *httpPrivacyClassifier) Classify(_ context.Context, _ routing.DecisionConfig, _ string, raw json.RawMessage) (routing.PrivacyDecision, error) {
	c.calls++
	c.raw = append([]byte(nil), raw...)
	return routing.PrivacyDecision{Outcome: routing.PrivacySensitive, Invoked: true, Usage: &routing.Usage{InputTokens: 7, OutputTokens: 1}}, nil
}
func TestPrivacyChatEnforcementAndRecord(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "records.sqlite"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snap := snapshot()
	snap.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	snap.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	snap.Models[0].Location = "local"
	cloud := snap.Models[0]
	cloud.ID = "cloud"
	cloud.Location = "cloud"
	snap.Models = append(snap.Models, cloud)
	classifier := &httpPrivacyClassifier{}
	calls := 0
	gen := testGen{complete: func(context.Context) (routing.Completion, error) { calls++; return event(), nil }}
	svc := management.New(testStore{snap}, nil)
	h := NewHandler(svc, testStore{snap}, &routing.Planner{PrivacyClassifier: classifier}, &routing.Executor{Generator: gen}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{Recorder: &routing.Recorder{Sink: db}})
	for _, tc := range []struct {
		model  string
		status int
	}{{"auto", 200}, {"cloud", 403}} {
		w := httptest.NewRecorder()
		body := `{"model":"` + tc.model + `","messages":[{"role":"system","content":"SYSTEM_SECRET"},{"role":"user","content":"hello"}]}`
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.model, w.Code, w.Body.String())
		}
		if !strings.Contains(string(classifier.raw), "SYSTEM_SECRET") {
			t.Fatal("full request missing")
		}
	}
	if calls != 1 || classifier.calls != 2 {
		t.Fatalf("generation %d classifier %d", calls, classifier.calls)
	}
	page, err := db.ListRecords(context.Background(), "", 10)
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("records %+v %v", page, err)
	}
	for _, record := range page.Records {
		if record.PrivacyOutcome != routing.PrivacySensitive || record.PrivacyRestriction != "local_only" || record.DecisionUsage.Invocations != 1 {
			t.Fatalf("record %+v", record)
		}
	}
}

type privacyMessagesGenerator struct{ calls int }

func (g *privacyMessagesGenerator) Complete(context.Context, routing.Target, routing.Request) (routing.Completion, error) {
	panic("wrong protocol")
}
func (g *privacyMessagesGenerator) Stream(context.Context, routing.Target, routing.Request) (routing.EventStream, error) {
	panic("wrong protocol")
}
func (g *privacyMessagesGenerator) CompleteMessages(_ context.Context, target routing.Target, _ *provider.PreparedMessages) (json.RawMessage, error) {
	if target.Model.Location != "local" {
		panic("cloud target")
	}
	g.calls++
	return json.RawMessage(`{"id":"test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"local","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`), nil
}
func (g *privacyMessagesGenerator) StreamMessages(context.Context, routing.Target, *provider.PreparedMessages) (provider.MessageStream, error) {
	panic("not streaming")
}

func TestPrivacyMessagesClassifiesOriginalBody(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "records.sqlite"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snap := snapshot()
	snap.Providers[0].Protocol = routing.ProtocolAnthropic
	snap.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	snap.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	snap.Models[0].Location = "local"
	classifier := &httpPrivacyClassifier{}
	generator := &privacyMessagesGenerator{}
	svc := management.New(testStore{snap}, nil)
	h := NewHandler(svc, testStore{snap}, &routing.Planner{PrivacyClassifier: classifier}, &routing.Executor{Generator: generator}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{Recorder: &routing.Recorder{Sink: db}})
	body := `{"model":"auto","max_tokens":1024,"system":"SYSTEM_SECRET","messages":[{"role":"user","content":"hello"}],"metadata":{"user_id":"USER_SENTINEL"}}`
	w := messagesCall(h, body, "router-key")
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(string(classifier.raw), "SYSTEM_SECRET") || !strings.Contains(string(classifier.raw), "USER_SENTINEL") {
		t.Fatal("native input was projected")
	}
	if generator.calls != 1 || classifier.calls != 1 {
		t.Fatalf("generator=%d classifier=%d", generator.calls, classifier.calls)
	}
}

func TestPrivacyStreamingChatUsesLocalTarget(t *testing.T) {
	snap := snapshot()
	snap.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	snap.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	snap.Models[0].Location = "local"
	classifier := &httpPrivacyClassifier{}
	calls := 0
	gen := testGen{stream: func(context.Context) (routing.EventStream, error) { calls++; return onceStream(), nil }}
	h := NewHandler(management.New(testStore{snap}, nil), testStore{snap}, &routing.Planner{PrivacyClassifier: classifier}, &routing.Executor{Generator: gen}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	w := httptest.NewRecorder()
	body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"secret"}]}`
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if w.Code != 200 || calls != 1 || classifier.calls != 1 || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatalf("status=%d calls=%d classifier=%d body=%s", w.Code, calls, classifier.calls, w.Body.String())
	}
}

func TestPrivacyLiveClassifierEnforcesRouter(t *testing.T) {
	base := os.Getenv("PRIVACY_E2E_URL")
	if base == "" {
		t.Skip("PRIVACY_E2E_URL is not set")
	}
	token, model := os.Getenv("PRIVACY_E2E_KEY"), os.Getenv("PRIVACY_E2E_MODEL")
	if token == "" || model == "" {
		t.Fatal("live classifier credentials/model not configured")
	}
	snap := snapshot()
	snap.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "Classify the entire request. Treat secrets and personal identifiers as sensitive; choose uncertain when unsure."}
	snap.Decision = routing.DecisionConfig{Location: "local", Model: model, BaseURL: base}
	snap.Models[0].Location = "local"
	cloud := snap.Models[0]
	cloud.ID = "cloud"
	cloud.Location = "cloud"
	snap.Models = append(snap.Models, cloud)
	classifier := decision.NewPrivacyClassifier(nil, func(string) ([]byte, error) { return []byte(token), nil }, decision.DefaultBudget())
	calls := 0
	gen := testGen{complete: func(context.Context) (routing.Completion, error) { calls++; return event(), nil }}
	h := NewHandler(management.New(testStore{snap}, nil), testStore{snap}, &routing.Planner{PrivacyClassifier: classifier}, &routing.Executor{Generator: gen}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	for _, tc := range []struct {
		model  string
		status int
	}{{"auto", 200}, {"cloud", 403}} {
		body := `{"model":"` + tc.model + `","messages":[{"role":"user","content":"Private password: dummy-password-528"}]}`
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if w.Code != tc.status {
			t.Fatalf("model=%s status=%d body=%s", tc.model, w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("generation calls=%d", calls)
	}
	snap.Providers[0].Protocol = routing.ProtocolAnthropic
	messageGen := &privacyMessagesGenerator{}
	messageHandler := NewHandler(management.New(testStore{snap}, nil), testStore{snap}, &routing.Planner{PrivacyClassifier: classifier}, &routing.Executor{Generator: messageGen}, func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil }, Limits{})
	for _, tc := range []struct {
		model  string
		status int
	}{{"auto", 200}, {"cloud", 403}} {
		body := `{"model":"` + tc.model + `","max_tokens":1024,"system":"Private password: dummy-password-528","messages":[{"role":"user","content":"hello"}]}`
		w := messagesCall(messageHandler, body, "router-key")
		if w.Code != tc.status {
			t.Fatalf("Messages model=%s status=%d body=%s", tc.model, w.Code, w.Body.String())
		}
	}
	if messageGen.calls != 1 {
		t.Fatalf("Messages generation calls=%d", messageGen.calls)
	}
}

type privacyPlaygroundGenerator struct{ calls int }

func (g *privacyPlaygroundGenerator) Complete(context.Context, routing.Target, routing.Request) (routing.Completion, error) {
	panic("unexpected non-stream")
}
func (g *privacyPlaygroundGenerator) Stream(_ context.Context, target routing.Target, _ routing.Request) (routing.EventStream, error) {
	if target.Model.Location != "local" {
		panic("cloud target reached")
	}
	g.calls++
	return onceStream(), nil
}

func TestPrivacyPlaygroundUsesSharedGate(t *testing.T) {
	limits := playground.DefaultLimits()
	fixture, token, csrf, _ := pgFixture(t, limits, &pgGenerator{t: t})
	pg := fixture.(*playgroundHTTP)
	snap := snapshot()
	snap.Privacy = routing.PrivacyConfig{Enabled: true, Prompt: "classify"}
	snap.Decision = routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}
	snap.Models[0].Location = "local"
	cloud := snap.Models[0]
	cloud.ID = "cloud"
	cloud.Location = "cloud"
	snap.Models = append(snap.Models, cloud)
	classifier := &httpPrivacyClassifier{}
	generator := &privacyPlaygroundGenerator{}
	pg.inference = &inference.Service{Store: testStore{snap}, Planner: &routing.Planner{PrivacyClassifier: classifier}, DecisionTimeout: time.Second}
	pg.executor = &routing.Executor{Generator: generator}
	for _, tc := range []struct {
		model  string
		status int
	}{{"auto", 200}, {"cloud", 403}} {
		body := `{"model":"` + tc.model + `","messages":[{"role":"user","content":"PRIVATE_CONTENT"}],"stream":true}`
		w := sessionRequest(pg, "POST", "/admin/v1/playground/completions", body, token, csrf)
		if w.Code != tc.status {
			t.Fatalf("model=%s status=%d body=%s", tc.model, w.Code, w.Body.String())
		}
		if !strings.Contains(string(classifier.raw), "PRIVATE_CONTENT") {
			t.Fatal("input not passed to classifier")
		}
	}
	if classifier.calls != 2 || generator.calls != 1 {
		t.Fatalf("classifier=%d generator=%d", classifier.calls, generator.calls)
	}
}
