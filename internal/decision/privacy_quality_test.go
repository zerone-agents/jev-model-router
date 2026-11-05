package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// This measures a small synthetic fixture, not an assurance of privacy.
func TestPrivacyQualityLive(t *testing.T) {
	base := os.Getenv("PRIVACY_E2E_URL")
	if base == "" {
		t.Skip("PRIVACY_E2E_URL is not set")
	}
	token, model := os.Getenv("PRIVACY_E2E_KEY"), os.Getenv("PRIVACY_E2E_MODEL")
	if token == "" || model == "" {
		t.Fatal("missing live configuration")
	}
	classifier := NewPrivacyClassifier(nil, func(string) ([]byte, error) { return []byte(token), nil }, DefaultBudget())
	prompt := "Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure."
	type sample struct {
		name      string
		sensitive bool
		messages  []map[string]any
	}
	user := func(s string) []map[string]any { return []map[string]any{{"role": "user", "content": s}} }
	noise := strings.Repeat("The weather is pleasant and the public meeting starts at noon. ", 35)
	samples := []sample{
		{"api_key", true, user("My private API key is dummy-secret-8294. Explain how to protect it.")},
		{"password", true, user("Password: mock-river-2841. Please help me remember it.")},
		{"contract", true, user("Confidential: our unreleased acquisition offer is $12 million.")},
		{"medical", true, user("Private patient Jane Example was diagnosed with diabetes.")},
		{"identity_zh", true, user("请保密：张示例的身份证号码是 110101199001011234。")},
		{"contract_zh", true, user("公司内部资料，未经授权禁止公开：下月收购方案。")},
		{"password_zh", true, user("我的登录密码是测试密码-8321，不要告诉别人。")},
		{"system", true, []map[string]any{{"role": "system", "content": "Private password mock-112233"}, {"role": "user", "content": "Hello"}}},
		{"developer", true, []map[string]any{{"role": "developer", "content": "Confidential project name: moonrise-dummy"}, {"role": "user", "content": "Hello"}}},
		{"tool_result", true, []map[string]any{{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}}, {"role": "tool", "tool_call_id": "c", "content": "Private token dummy-token-392"}, {"role": "user", "content": "Summarize."}}},
		{"tool_arguments", true, []map[string]any{{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{\"password\":\"dummy-5326\"}"}}}}, {"role": "user", "content": "Hello"}}},
		{"long_history_start", true, []map[string]any{{"role": "user", "content": "Private password dummy-hello-12. " + noise}, {"role": "assistant", "content": "Understood."}, {"role": "user", "content": "Tell me more."}}},
		{"long_history_middle", true, user(noise + " Confidential internal strategy: project dummy-x. " + noise)},
		{"long_history_end", true, user(noise + " This is private: dummy password 4567.")},
		{"public_math", false, user("What is 12 times 12?")},
		{"public_zh", false, user("请解释为什么天空是蓝色的。")},
		{"public_code", false, user("Write a Python function that adds two integers.")},
		{"public_history", false, user("Summarize the history of public libraries.")},
		{"public_recipe", false, user("给我一份简单的番茄炒蛋食谱。")},
		{"public_weather", false, user("Explain how rain forms.")},
	}
	falseNeg, uncertain, falsePos, skipped := 0, 0, 0, 0
	start := time.Now()
	for _, s := range samples {
		raw, _ := json.Marshal(map[string]any{"model": "auto", "messages": s.messages})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		got, err := classifier.Classify(ctx, routing.DecisionConfig{BaseURL: base, Model: model, Location: "local"}, prompt, raw)
		cancel()
		if err != nil || got.Outcome == routing.PrivacyUncertain {
			uncertain++
		}
		if !got.Invoked {
			skipped++
		}
		if s.sensitive && got.Outcome == routing.PrivacyNonSensitive {
			falseNeg++
		}
		if !s.sensitive && got.Outcome == routing.PrivacySensitive {
			falsePos++
		}
		t.Logf("%s expected_sensitive=%t outcome=%s invoked=%t error=%t", s.name, s.sensitive, got.Outcome, got.Invoked, err != nil)
	}
	fmt.Printf("quality n=%d sensitive=14 false_negative=%d uncertain_or_error=%d false_positive=%d preflight_rejected=%d elapsed=%s\n", len(samples), falseNeg, uncertain, falsePos, skipped, time.Since(start))
}
