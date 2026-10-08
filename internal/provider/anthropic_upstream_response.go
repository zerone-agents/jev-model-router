package provider

import (
	"encoding/json"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"math"
	"reflect"
	"strings"
)

func nativeRaw(v any) json.RawMessage {
	switch v := v.(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	case json.RawMessage:
		return v
	default:
		b, _ := json.Marshal(v)
		return b
	}
}

type nativeUsage struct {
	Input  *int64 `json:"input_tokens"`
	Output *int64 `json:"output_tokens"`
	Read   *int64 `json:"cache_read_input_tokens"`
	Write  *int64 `json:"cache_creation_input_tokens"`
}

func (u nativeUsage) normalized() (*routing.Usage, error) {
	if u.Input == nil || u.Output == nil {
		return nil, nativeError(502)
	}
	total := int64(0)
	for _, v := range []*int64{u.Input, u.Read, u.Write, u.Output} {
		if v != nil {
			if *v < 0 || *v > math.MaxInt64-total {
				return nil, nativeError(502)
			}
			total += *v
		}
	}
	result := &routing.Usage{InputTokens: total - *u.Output, OutputTokens: *u.Output, TotalTokens: total}
	details := map[string]int64{}
	if u.Read != nil {
		details["cached_tokens"] = *u.Read
	}
	if u.Write != nil {
		details["cache_creation_tokens"] = *u.Write
	}
	if len(details) > 0 {
		result.InputDetails, _ = json.Marshal(details)
	}
	return result, nil
}

type nativeBlock struct {
	Type      string          `json:"type"`
	Text      *string         `json:"text"`
	Thinking  *string         `json:"thinking"`
	Signature *string         `json:"signature"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}
type nativeResponse struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []nativeBlock `json:"content"`
	Stop    string        `json:"stop_reason"`
	Usage   *nativeUsage  `json:"usage"`
}

func nativeFinish(stop string) (string, error) {
	switch stop {
	case "end_turn", "stop_sequence":
		return "stop", nil
	case "max_tokens":
		return "length", nil
	case "tool_use":
		return "tool_calls", nil
	default:
		return "", nativeError(502)
	}
}
func anthropicUpstreamCompletion(in *schemas.BifrostChatResponse, raw json.RawMessage, tools bool) (routing.Completion, error) {
	var body nativeResponse
	if json.Unmarshal(raw, &body) != nil || body.Type != "message" || body.Role != "assistant" || body.ID == "" {
		return routing.Completion{}, nativeError(502)
	}
	finish, e := nativeFinish(body.Stop)
	if e != nil {
		return routing.Completion{}, e
	}
	var text, reason strings.Builder
	var calls []routing.ToolCall
	ids := map[string]bool{}
	hasThinking := false
	hasText := false
	for _, b := range body.Content {
		switch b.Type {
		case "text":
			if b.Text == nil {
				return routing.Completion{}, nativeError(502)
			}
			hasText = true
			text.WriteString(*b.Text)
		case "thinking":
			if b.Thinking == nil {
				return routing.Completion{}, nativeError(502)
			}
			hasThinking = true
			reason.WriteString(*b.Thinking)
		case "tool_use":
			var object map[string]any
			if b.ID == "" || b.Name == "" || ids[b.ID] || json.Unmarshal(b.Input, &object) != nil || object == nil {
				return routing.Completion{}, nativeError(502)
			}
			ids[b.ID] = true
			calls = append(calls, routing.ToolCall{ID: b.ID, Type: "function", Function: routing.CallFunction{Name: b.Name, Arguments: string(b.Input)}})
		default:
			return routing.Completion{}, nativeError(502)
		}
	}
	if len(body.Content) == 0 || hasThinking && (tools || len(calls) > 0) || (finish == "tool_calls") != (len(calls) > 0) {
		return routing.Completion{}, nativeError(502)
	}
	out, e := completion(in)
	if e != nil || len(out.Choices) != 1 || out.Choices[0].Message == nil {
		return routing.Completion{}, nativeError(502)
	}
	m := out.Choices[0].Message
	var gotText string
	json.Unmarshal(m.Content, &gotText)
	if gotText != text.String() || len(m.ToolCalls) != len(calls) {
		return routing.Completion{}, nativeError(502)
	}
	if hasThinking {
		var converted strings.Builder
		if in.Choices[0].Message != nil && in.Choices[0].Message.ChatAssistantMessage != nil {
			for _, detail := range in.Choices[0].Message.ReasoningDetails {
				if detail.Text != nil {
					converted.WriteString(*detail.Text)
				}
			}
		}
		if converted.String() != reason.String() {
			return routing.Completion{}, nativeError(502)
		}
		// Bifrost adds a newline after each thinking block in its summary string.
		// Its structured details preserve the exact text; expose those instead.
		value := converted.String()
		m.ReasoningContent = &value
	}
	for i, c := range calls {
		got := m.ToolCalls[i]
		if got.ID != c.ID || got.Function.Name != c.Function.Name || !reflect.DeepEqual(decoded([]byte(got.Function.Arguments)), decoded([]byte(c.Function.Arguments))) {
			return routing.Completion{}, nativeError(502)
		}
	}
	if !hasText && len(calls) == 0 && !hasThinking {
		return routing.Completion{}, nativeError(502)
	}
	out.Choices[0].FinishReason = &finish
	out.Usage = nil
	if body.Usage != nil {
		out.Usage, e = body.Usage.normalized()
	}
	return out, e
}
