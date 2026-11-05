package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// NewPrivacyClassifier uses the same configured decision service as selection.
func NewPrivacyClassifier(client *http.Client, resolve func(string) ([]byte, error), budget BudgetPolicy) routing.PrivacyClassifier {
	return New(client, resolve, budget).(*jev)
}

func (j *jev) Classify(ctx context.Context, config routing.DecisionConfig, prompt string, raw json.RawMessage) (routing.PrivacyDecision, error) {
	result := routing.PrivacyDecision{Outcome: routing.PrivacyUncertain}
	if config.Location != "local" || config.BaseURL == "" || config.Model == "" {
		return result, nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || !inspectableRequest(value) || containsUnsupportedMedia(value) {
		return result, nil
	}
	body, err := json.Marshal(map[string]any{"model": config.Model, "state": json.RawMessage(raw), "questions": map[string]any{"privacy": map[string]any{"type": "choice", "instructions": prompt, "criteria": map[string]string{"non_sensitive": "No sensitive information is present.", "sensitive": "Sensitive or confidential information is present.", "uncertain": "The sensitivity cannot be determined reliably."}}}})
	if err != nil || len(body) > j.budget.MaxBytes {
		return result, nil
	}
	result.Invoked = true
	key, err := j.resolve(config.SecretRef)
	if err != nil {
		return result, routing.Fail("config_missing", "decision credential unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(config.BaseURL, "/")+decisionPath(config), bytes.NewReader(body))
	if err != nil {
		return result, routing.Fail("config_missing", "invalid decision endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+string(key))
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.client.Do(req)
	if err != nil {
		return result, decisionIOError(ctx, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result, decisionIOError(ctx, err)
	}
	var envelope struct {
		Answers map[string]struct{ Type, Choice string }
		Usage   json.RawMessage
	}
	if json.Unmarshal(b, &envelope) == nil {
		result.Usage = parseDecisionUsage(envelope.Usage, config)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, routing.Fail("upstream_error", "privacy classification failed")
	}
	a, ok := envelope.Answers["privacy"]
	if !ok || a.Type != "choice" {
		return result, routing.Fail("invalid_decision", "invalid privacy classification")
	}
	switch routing.PrivacyOutcome(a.Choice) {
	case routing.PrivacyNonSensitive, routing.PrivacySensitive, routing.PrivacyUncertain:
		result.Outcome = routing.PrivacyOutcome(a.Choice)
	default:
		return result, routing.Fail("invalid_decision", "invalid privacy classification")
	}
	return result, nil
}

// The currently supported local classifiers accept text only; never fetch media.
func containsUnsupportedMedia(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if t, ok := v["type"].(string); ok {
			switch t {
			case "image", "image_url", "input_image", "audio", "input_audio", "video", "document", "file":
				return true
			}
		}
		for _, child := range v {
			if containsUnsupportedMedia(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsUnsupportedMedia(child) {
				return true
			}
		}
	}
	return false
}

// inspectableRequest recognizes only content forms the local text classifier
// can inspect. Unknown or redacted blocks must never be classified as safe.
func inspectableRequest(value any) bool {
	root, ok := value.(map[string]any)
	if !ok {
		return false
	}
	messages, ok := root["messages"].([]any)
	if !ok || len(messages) == 0 {
		return false
	}
	if system, exists := root["system"]; exists && !inspectableContent(system, false) {
		return false
	}
	for _, entry := range messages {
		msg, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		if content, exists := msg["content"]; exists && content != nil && !inspectableContent(content, false) {
			return false
		}
	}
	return true
}
func inspectableContent(value any, nested bool) bool {
	switch content := value.(type) {
	case string:
		return true
	case []any:
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				return false
			}
			typ, ok := block["type"].(string)
			if !ok {
				return false
			}
			switch typ {
			case "text":
				if _, ok := block["text"].(string); !ok {
					return false
				}
			case "thinking":
				if _, ok := block["thinking"].(string); !ok {
					return false
				}
			case "tool_use":
				if nested {
					return false
				}
				if _, ok := block["input"]; !ok {
					return false
				}
			case "tool_result":
				if nested {
					return false
				}
				if v, ok := block["content"]; ok && !inspectableContent(v, true) {
					return false
				}
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}
