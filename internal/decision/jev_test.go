package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func input() routing.DecisionInput {
	return routing.DecisionInput{Prompt: "Balanced", Candidates: []routing.Model{{ID: "fast", Description: "Fast"}, {ID: "deep", Description: "Reasoning"}}, Request: routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}}}
}
func TestNativeChoiceMapping(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer key" {
			t.Error("wrong request")
		}
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["questions"]) == 0 {
			t.Error("missing questions")
		}
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m1"}},"usage":{"input_tokens":12,"output_tokens":1}}`))
	}))
	defer s.Close()
	d, e := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL, Model: "jev-1.13.0"}, input())
	if e != nil || d.ModelID != "deep" || d.Usage.InputTokens != 12 {
		t.Fatalf("%+v %v", d, e)
	}
}
func TestUnknownChoiceRejected(t *testing.T) {
	for _, body := range []string{`{"answers":{"model":{"type":"choice","choice":"m9"}}}`, `{"answers":{"model":{"type":"text","choice":"m0"}}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		_, e := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL}, input())
		s.Close()
		if e == nil {
			t.Fatal("invalid choice accepted")
		}
	}
}
func TestCandidateLimitRejected(t *testing.T) {
	i := input()
	i.Candidates = make([]routing.Model, 256)
	if _, e := BuildState(i, DefaultBudget()); e == nil {
		t.Fatal("too many candidates")
	}
}
func TestLatestTaskOverflow(t *testing.T) {
	i := input()
	i.Request.Messages[0].Content, _ = json.Marshal(strings.Repeat("x", 40000))
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer s.Close()
	_, e := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL}, i)
	if e == nil || calls != 0 {
		t.Fatal("overflow reached Jev")
	}
}
func TestImageExcludedFromDecision(t *testing.T) {
	i := input()
	i.Request.Messages[0].Content = json.RawMessage(`[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://secret.invalid/IMAGE_SENTINEL"}}]`)
	b, e := BuildState(i, DefaultBudget())
	if e != nil || bytes.Contains(b, []byte("IMAGE_SENTINEL")) || !bytes.Contains(b, []byte("image_present")) {
		t.Fatalf("%s %v", b, e)
	}
}
func TestPairedToolHistory(t *testing.T) {
	i := input()
	i.Request.Messages = []routing.Message{{Role: "user", Content: json.RawMessage(`"old"`)}, {Role: "assistant", ToolCalls: []routing.ToolCall{{ID: "call", Type: "function", Function: routing.CallFunction{Name: "f", Arguments: strings.Repeat("x", 5000)}}}}, {Role: "tool", ToolCallID: "call", Content: json.RawMessage(`"result"`)}, {Role: "user", Content: json.RawMessage(`"latest"`)}}
	p := DefaultBudget()
	p.MaxBytes = 2000
	b, e := BuildState(i, p)
	if e != nil || bytes.Contains(b, []byte(`"role":"tool"`)) || !bytes.Contains(b, []byte("latest")) || !bytes.Contains(b, []byte("omitted")) {
		t.Fatalf("%s %v", b, e)
	}
}
func TestNoDecisionRetry(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503); w.Write([]byte("SECRET")) }))
	defer s.Close()
	_, e := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL}, input())
	if e == nil || strings.Contains(e.Error(), "SECRET") || calls != 1 {
		t.Fatal("unsafe retry/error")
	}
}
