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
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}}}`))
	}))
	defer s.Close()
	_, e := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL}, i)
	if e != nil || calls != 1 {
		t.Fatalf("truncated task should reach Jev: %v", e)
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
	i.Request.Messages = []routing.Message{{Role: "user", Content: json.RawMessage(`"` + strings.Repeat("old", 2000) + `"`)}, {Role: "assistant", ToolCalls: []routing.ToolCall{{ID: "call", Type: "function", Function: routing.CallFunction{Name: "f", Arguments: strings.Repeat("x", 5000)}}}}, {Role: "tool", ToolCallID: "call", Content: json.RawMessage(`"result"`)}, {Role: "user", Content: json.RawMessage(`"latest"`)}}
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

func TestDecisionIncludesOutputAndToolRequirements(t *testing.T) {
	i := input()
	i.Request.Options = map[string]json.RawMessage{"response_format": json.RawMessage(`{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object","properties":{"OUTPUT_SENTINEL":{"type":"string"}}}}}`), "tool_choice": json.RawMessage(`"required"`), "parallel_tool_calls": json.RawMessage(`false`), "max_completion_tokens": json.RawMessage(`128`)}
	b, e := BuildState(i, DefaultBudget())
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"OUTPUT_SENTINEL", `"tool_choice":"required"`, `"parallel_tool_calls":false`, `"max_completion_tokens":128`} {
		if !bytes.Contains(b, []byte(part)) {
			t.Fatalf("missing %s", part)
		}
	}
}
func TestOutputRequirementsConsumeRequiredBudget(t *testing.T) {
	i := input()
	format := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "result", "schema": map[string]any{"description": strings.Repeat("x", DefaultMaxBytes)}}}
	raw, _ := json.Marshal(format)
	i.Request.Options = map[string]json.RawMessage{"response_format": raw}
	if _, e := BuildState(i, DefaultBudget()); e == nil {
		t.Fatal("oversized required output schema accepted")
	}
}

func TestReasoningRequirementsConsumeBudget(t *testing.T) {
	in := input()
	base, err := BuildState(in, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	q, _ := json.Marshal(question(in))
	p := DefaultBudget()
	p.MaxBytes = len(base) + len(q) + 512
	if _, err := BuildState(in, p); err != nil {
		t.Fatal(err)
	}
	in.Request.Options = map[string]json.RawMessage{"chat_template_kwargs": json.RawMessage(`{"enable_thinking":true}`), "reasoning_effort": json.RawMessage(`"high"`)}
	out, err := BuildState(in, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"chat_template_kwargs":{"enable_thinking":true}`, `"reasoning_effort":"high"`} {
		if !bytes.Contains(out, []byte(part)) {
			t.Fatalf("missing requirement %s", part)
		}
	}
	if _, err := BuildState(in, p); err == nil {
		t.Fatal("reasoning requirements omitted from budget")
	}
}

func TestConfigurableDecisionByteBudget(t *testing.T) {
	in := input()
	in.Prompt = strings.Repeat("x", 25000)
	small := DefaultBudget()
	small.MaxBytes = 24000
	if _, err := BuildState(in, small); err == nil {
		t.Fatal("small budget should reject")
	}
	if _, err := BuildState(in, DefaultBudget()); err != nil {
		t.Fatal(err)
	}
	in.Prompt = strings.Repeat("x", 40000)
	if _, err := BuildState(in, DefaultBudget()); err == nil {
		t.Fatal("default should still bound input")
	}
	large := DefaultBudget()
	large.MaxBytes = 64000
	if _, err := BuildState(in, large); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}}}`))
	}))
	defer server.Close()
	_, err := New(server.Client(), func(string) ([]byte, error) { return []byte("test"), nil }, large).Choose(context.Background(), routing.DecisionConfig{BaseURL: server.URL}, in)
	if err != nil || calls != 1 {
		t.Fatalf("configured send budget: calls=%d err=%v", calls, err)
	}
}

func TestDecisionOnlyKeepsToolRecords(t *testing.T) {
	in := input()
	description := strings.Repeat("DEFINITION_SENTINEL", 5000)
	in.Request.Tools = []routing.Tool{{Type: "function", Function: routing.Function{Name: "Read", Description: &description, Parameters: json.RawMessage(`{"SCHEMA_SENTINEL":true}`)}}}
	thought := "THOUGHT_SENTINEL"
	in.Request.Messages = []routing.Message{
		{Role: "system", Content: json.RawMessage(`"system preserved"`)},
		{Role: "user", Content: json.RawMessage(`"latest task preserved"`)},
		{Role: "assistant", Content: json.RawMessage(`"CALL_TEXT_SENTINEL"`), ReasoningContent: &thought, ToolCalls: []routing.ToolCall{
			{ID: "call1", Type: "function", Function: routing.CallFunction{Name: "Read", Arguments: strings.Repeat("ARG_SENTINEL", 5000)}},
			{ID: "call2", Type: "function", Function: routing.CallFunction{Name: "Bash", Arguments: "OTHER_ARG_SENTINEL"}},
		}},
		{Role: "tool", ToolCallID: "call1", Content: json.RawMessage(`"` + strings.Repeat("RESULT_SENTINEL", 5000) + `"`)},
		{Role: "tool", ToolCallID: "call2", Content: json.RawMessage(`[{"type":"text","text":"ARRAY_RESULT_SENTINEL"}]`)},
	}
	before, _ := json.Marshal(in)
	b, err := BuildState(in, BudgetPolicy{MaxBytes: 3000})
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	if _, exists := state["tools"]; exists {
		t.Fatal("tool definitions included in decision state")
	}
	if bytes.Contains(b, []byte("SENTINEL")) || bytes.Contains(b, []byte(`"arguments":`)) {
		t.Fatalf("tool payload leaked: %s", b)
	}
	for _, value := range []string{"system preserved", "latest task preserved", "Read", "Bash", `"tool_call_id":"call1"`, `"tool_call_id":"call2"`, `"id":"call1"`, `"id":"call2"`} {
		if !bytes.Contains(b, []byte(value)) {
			t.Errorf("missing record %s", value)
		}
	}
	after, _ := json.Marshal(in)
	if !bytes.Equal(before, after) {
		t.Fatal("generation input mutated")
	}
}

func TestIndependentMiddleTruncation(t *testing.T) {
	in := input()
	for _, role := range []string{"system", "developer", "user"} {
		content, _ := json.Marshal(role + "-HEAD" + strings.Repeat("中🙂\"\n", 10000) + role + "-TAIL")
		in.Request.Messages = append(in.Request.Messages, routing.Message{Role: role, Content: content})
	}
	before, _ := json.Marshal(in)
	b, err := BuildState(in, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	q, _ := json.Marshal(question(in))
	if len(b)+len(q)+512 > DefaultMaxBytes {
		t.Fatal("over budget")
	}
	for _, role := range []string{"system", "developer", "user"} {
		for _, end := range []string{"-HEAD", "-TAIL"} {
			if !bytes.Contains(b, []byte(role+end)) {
				t.Fatalf("lost %s%s", role, end)
			}
		}
	}
	if !bytes.Contains(b, []byte("[...truncated...]")) {
		t.Fatal("missing truncation marker")
	}
	after, _ := json.Marshal(in)
	if !bytes.Equal(before, after) {
		t.Fatal("generation input mutated")
	}
}

func TestMiddleTruncationPreservesPartsAndShortText(t *testing.T) {
	in := input()
	long := "HEAD" + strings.Repeat("x", 40000) + "TAIL"
	parts, _ := json.Marshal([]map[string]any{{"type": "text", "text": long}, {"type": "image_url", "image_url": map[string]string{"url": "secret"}}, {"type": "text", "text": "short unchanged"}})
	in.Request.Messages = []routing.Message{{Role: "system", Content: json.RawMessage(`"short system"`)}, {Role: "user", Content: parts}, {Role: "assistant", ReasoningContent: &long}}
	before, _ := json.Marshal(in)
	b, err := BuildState(in, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"short system", "short unchanged", "HEAD", "TAIL", "image_present"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Fatalf("missing %s", want)
		}
	}
	if bytes.Contains(b, []byte("secret")) {
		t.Fatal("image payload leaked")
	}
	after, _ := json.Marshal(in)
	if !bytes.Equal(before, after) {
		t.Fatal("generation input mutated")
	}
}

func TestMinimumTruncationKeepsShortText(t *testing.T) {
	in := input()
	long, _ := json.Marshal(strings.Repeat("x", 10000))
	in.Request.Messages = []routing.Message{{Role: "system", Content: json.RawMessage(`"abc"`)}, {Role: "user", Content: long}}
	b, err := BuildState(in, BudgetPolicy{MaxBytes: 1373})
	if err != nil {
		t.Fatal(err)
	}
	var got state
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Messages[0].Content) != `"abc"` {
		t.Fatalf("short text changed: %s", got.Messages[0].Content)
	}
	q, _ := json.Marshal(question(in))
	if len(b)+len(q)+512 > 1373 {
		t.Fatal("over budget")
	}
}

func TestMiddleTruncationSerializedSizeIsMonotone(t *testing.T) {
	for _, text := range []string{"abc", strings.Repeat("x", 100), strings.Repeat("中🙂\"\n<>&", 30), "abc" + strings.Repeat("\u0000", 30) + "xyz"} {
		original, _ := json.Marshal(text)
		previous := 0
		chars := []rune(text)
		for cap := 0; cap <= len(chars)+1; cap++ {
			got := middleTruncate(text, chars, cap)
			encoded, _ := json.Marshal(got)
			if len(encoded) < previous || len(encoded) > len(original) {
				t.Fatalf("nonmonotone or expanded text at cap %d: previous=%d actual=%d original=%d", cap, previous, len(encoded), len(original))
			}
			previous = len(encoded)
		}
	}
}
