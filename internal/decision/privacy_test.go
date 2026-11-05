package decision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestPrivacyClassificationPreservesCompleteRequest(t *testing.T) {
	raw := json.RawMessage(`{"model":"auto","messages":[{"role":"system","content":"SYSTEM_SECRET"},{"role":"assistant","tool_calls":[{"function":{"arguments":"TOOL_ARGS"}}]},{"role":"tool","content":"TOOL_RESULT"}],"tools":[{"name":"TOOL_DEFINITION"}]}`)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		for _, word := range []string{"SYSTEM_SECRET", "TOOL_ARGS", "TOOL_RESULT", "TOOL_DEFINITION"} {
			if !strings.Contains(string(b), word) {
				t.Errorf("missing %s", word)
			}
		}
		w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"usage":{"input_tokens":21,"output_tokens":1}}`))
	}))
	defer server.Close()
	classifier := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget())
	result, err := classifier.Classify(context.Background(), routing.DecisionConfig{BaseURL: server.URL, Model: "local", Location: "local"}, "classify", raw)
	if err != nil || result.Outcome != routing.PrivacySensitive || !result.Invoked || result.Usage == nil || result.Usage.InputTokens != 21 || calls != 1 {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
	}
}

func TestPrivacyClassificationFailsClosedBeforeInvocation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	classifier := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget())
	cfg := routing.DecisionConfig{BaseURL: server.URL, Model: "local", Location: "local"}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image"}}]}]}`), json.RawMessage(`{"messages":[{"role":"user","content":"` + strings.Repeat("x", 40000) + `"}]}`)} {
		got, err := classifier.Classify(context.Background(), cfg, "classify", raw)
		if err != nil || got.Outcome != routing.PrivacyUncertain || got.Invoked {
			t.Fatalf("got %+v, err %v", got, err)
		}
	}
	if calls != 0 {
		t.Fatalf("sent %d unsupported requests", calls)
	}
}

func TestPrivacyClassificationInvalidResponsesRemainUncertain(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unknown choice", `{"answers":{"privacy":{"type":"choice","choice":"other"}},"usage":{"input_tokens":8,"output_tokens":1}}`, 200},
		{"missing choice", `{"answers":{}}`, 200},
		{"partially decoded answers", `{"answers":{"privacy":{"type":"choice","choice":"non_sensitive"},"extra":42},"usage":{"input_tokens":2,"output_tokens":1}}`, 200},
		{"upstream error", `{"error":{"message":"SECRET"}}`, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			classifier := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget())
			got, err := classifier.Classify(context.Background(), routing.DecisionConfig{BaseURL: server.URL, Model: "local", Location: "local"}, "classify", json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`))
			if err == nil || got.Outcome != routing.PrivacyUncertain || !got.Invoked || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("got %+v err %v", got, err)
			}
		})
	}

}

func TestPrivacyClassificationRejectsUnknownContentBlocks(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"non_sensitive"}}}`))
	}))
	defer server.Close()
	classifier := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget())
	for _, raw := range []string{
		`{"messages":[{"role":"user","content":[{"type":"future_media","data":"SECRET"}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"future_media","data":"SECRET"}]}]}]}`,
		`{"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"SECRET"}]}]}`,
	} {
		got, err := classifier.Classify(context.Background(), routing.DecisionConfig{BaseURL: server.URL, Model: "local", Location: "local"}, "classify", json.RawMessage(raw))
		if err != nil || got.Outcome != routing.PrivacyUncertain || got.Invoked {
			t.Fatalf("unexpected result %+v err %v", got, err)
		}
	}
	if calls != 0 {
		t.Fatalf("unsupported content sent %d times", calls)
	}
}

func TestPrivacyClassificationTimeoutRemainsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer server.Close()
	classifier := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := classifier.Classify(ctx, routing.DecisionConfig{BaseURL: server.URL, Model: "local", Location: "local"}, "classify", json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err == nil || got.Outcome != routing.PrivacyUncertain || !got.Invoked {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestPrivacyClassificationLive(t *testing.T) {
	base := os.Getenv("PRIVACY_E2E_URL")
	if base == "" {
		t.Skip("PRIVACY_E2E_URL is not set")
	}
	token := os.Getenv("PRIVACY_E2E_KEY")
	model := os.Getenv("PRIVACY_E2E_MODEL")
	if token == "" || model == "" {
		t.Fatal("live classifier credentials/model not configured")
	}
	classifier := NewPrivacyClassifier(nil, func(string) ([]byte, error) { return []byte(token), nil }, DefaultBudget())
	for _, tc := range []struct {
		name, text string
		want       routing.PrivacyOutcome
	}{
		{"synthetic secret", "A private API key is sk-demo-example-secret-123.", routing.PrivacySensitive},
		{"benign", "What is two plus two?", routing.PrivacyNonSensitive},
		{"chinese", "请保密：身份证号码 110101199001011234。", routing.PrivacySensitive},
		{"chinese confidential", "这是内部未公开的客户合同，严禁对外披露。", routing.PrivacySensitive},
		{"english password", "My account password is pretend-password-824; help me remember it.", routing.PrivacySensitive},
		{"english public", "Summarize the public history of the internet.", routing.PrivacyNonSensitive},
		{"chinese public", "请解释太阳为什么从东方升起。", routing.PrivacyNonSensitive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": tc.text}}})
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			result, err := classifier.Classify(ctx, routing.DecisionConfig{BaseURL: base, Model: model, Location: "local"}, "Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.", raw)
			if err != nil || !result.Invoked || result.Outcome != tc.want {
				t.Fatalf("outcome=%s invoked=%v err=%v", result.Outcome, result.Invoked, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"system", `{"model":"auto","messages":[{"role":"system","content":"Confidential internal project code: secret-launch-47. Do not disclose."},{"role":"user","content":"Summarize our project."}]}`},
		{"tool result", `{"model":"auto","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"Private customer password: dummy-password-528"},{"role":"user","content":"Summarize the result."}]}`},
		{"history", `{"model":"auto","messages":[{"role":"user","content":"请保密，客户身份证号码 110101199001011234。"},{"role":"assistant","content":"好的。"},{"role":"user","content":"你好"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			result, err := classifier.Classify(ctx, routing.DecisionConfig{BaseURL: base, Model: model, Location: "local"}, "Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.", json.RawMessage(tc.raw))
			if err != nil || !result.Invoked || result.Outcome != routing.PrivacySensitive {
				t.Fatalf("outcome=%s invoked=%v err=%v", result.Outcome, result.Invoked, err)
			}
		})
	}
}

func TestPrivacyCredentialFailureCountsAdapterInvocation(t *testing.T) {
	classifier := NewPrivacyClassifier(nil, func(string) ([]byte, error) { return nil, errors.New("credential unavailable") }, DefaultBudget())
	cfg := routing.DecisionConfig{BaseURL: "http://127.0.0.1", Model: "local", Location: "local"}
	got, err := classifier.Classify(context.Background(), cfg, "classify", json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err == nil || !got.Invoked || got.Outcome != routing.PrivacyUncertain {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestPrivacyPreservesLongHistoryAndToolInputsExactly(t *testing.T) {
	for _, position := range []string{"start", "middle", "end"} {
		t.Run(position, func(t *testing.T) {
			filler := strings.Repeat("public context ", 400)
			secret := "SYNTHETIC_PRIVATE_VALUE"
			content := filler + secret
			if position == "start" {
				content = secret + filler
			}
			if position == "middle" {
				content = filler[:len(filler)/2] + secret + filler[len(filler)/2:]
			}
			payload := map[string]any{"model": "auto", "messages": []any{
				map[string]any{"role": "developer", "content": "developer confidential"},
				map[string]any{"role": "user", "content": content},
				map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{\"password\":\"SYNTHETIC_ARG\"}"}}}},
				map[string]any{"role": "tool", "tool_call_id": "c", "content": "SYNTHETIC_RESULT"},
			}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "description": "SYNTHETIC_SCHEMA", "parameters": map[string]any{"type": "object"}}}}}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				var request struct {
					State json.RawMessage `json:"state"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var got any
				if err := json.Unmarshal(request.State, &got); err != nil || !reflect.DeepEqual(got, payload) {
					t.Errorf("state changed: %v", err)
				}
				w.Write([]byte(`{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"usage":{"input_tokens":1,"output_tokens":0}}`))
			}))
			defer server.Close()
			c := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("fake"), nil }, DefaultBudget())
			result, err := c.Classify(context.Background(), routing.DecisionConfig{Location: "local", BaseURL: server.URL, Model: "local"}, "classify", raw)
			if err != nil || !called || result.Outcome != routing.PrivacySensitive {
				t.Fatalf("result=%+v called=%t err=%v", result, called, err)
			}
		})
	}
}

func TestPrivacyClassificationRejectsAmbiguousAnswersAndRetainsUsage(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"duplicate choice", `{"answers":{"privacy":{"type":"choice","choice":"sensitive","choice":"non_sensitive"}},"usage":{"input_tokens":17,"output_tokens":1}}`},
		{"duplicate privacy", `{"answers":{"privacy":{"type":"choice","choice":"sensitive"},"privacy":{"type":"choice","choice":"non_sensitive"}},"usage":{"input_tokens":17,"output_tokens":1}}`},
		{"duplicate answers", `{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"answers":{"privacy":{"type":"choice","choice":"non_sensitive"}},"usage":{"input_tokens":17,"output_tokens":1}}`},
		{"unicode folded answers", `{"answers":{"privacy":{"type":"choice","choice":"sensitive"}},"anſwers":{"privacy":{"type":"choice","choice":"non_sensitive"}},"usage":{"input_tokens":17,"output_tokens":1}}`},
		{"unicode folded choice", `{"answers":{"privacy":{"type":"choice","choice":"sensitive","choıce":"non_sensitive","CHOICE":"non_sensitive"}},"usage":{"input_tokens":17,"output_tokens":1}}`},
		{"invalid choice type", `{"answers":{"privacy":{"type":"choice","choice":42}},"usage":{"input_tokens":17,"output_tokens":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			c := NewPrivacyClassifier(server.Client(), func(string) ([]byte, error) { return []byte("test"), nil }, DefaultBudget())
			got, err := c.Classify(context.Background(), routing.DecisionConfig{Location: "local", BaseURL: server.URL, Model: "local"}, "classify", json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`))
			if err == nil || got.Outcome != routing.PrivacyUncertain || got.Usage == nil || got.Usage.InputTokens != 17 {
				t.Fatalf("result=%+v error=%v", got, err)
			}
		})
	}
}
