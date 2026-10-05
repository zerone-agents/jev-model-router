package routing

import (
	"bytes"
	"encoding/json"
	"math"
)

// ContextEstimate is a provider-neutral heuristic, not tokenizer output.
// Text uses UTF-8 bytes/4; schemas use compact JSON bytes and tool arguments
// use decoded argument-string bytes. Images and framing are separate reserves.
type ContextEstimate struct {
	Method          string `json:"method"`
	TextBytes       int64  `json:"text_bytes"`
	StructuredBytes int64  `json:"structured_bytes"`
	ImageTokens     int64  `json:"image_tokens"`
	FramingTokens   int64  `json:"framing_tokens"`
	InputTokens     int64  `json:"input_tokens"`
	OutputReserve   int64  `json:"output_reserve"`
	TotalTokens     int64  `json:"total_tokens"`
}

func saturatedSum(values ...int64) int64 {
	var total int64
	for _, value := range values {
		if value > 0 {
			if total > math.MaxInt64-value {
				return math.MaxInt64
			}
			total += value
		}
	}
	return total
}
func EstimateContext(_ Model, r Request) (int64, bool, error) {
	e, err := EstimateRequestContext(r)
	return e.TotalTokens, false, err
}
func EstimateRequestContext(r Request) (ContextEstimate, error) {
	e := ContextEstimate{Method: "semantic_bytes_v1", OutputReserve: 4096}
	text := func(s string) { e.TextBytes = saturatedSum(e.TextBytes, int64(len(s))) }
	structure := func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return nil
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw) != nil {
			return Fail("invalid_request", "cannot estimate context")
		}
		e.StructuredBytes = saturatedSum(e.StructuredBytes, int64(compact.Len()))
		return nil
	}
	content := func(raw json.RawMessage) error {
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			text(value)
			return nil
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return Fail("invalid_request", "cannot estimate context")
		}
		for _, part := range parts {
			switch part.Type {
			case "text":
				text(part.Text)
			case "image_url":
				e.ImageTokens = saturatedSum(e.ImageTokens, 8192)
			default:
				return Fail("unsupported_request", "cannot estimate content type")
			}
		}
		return nil
	}
	for _, m := range r.Messages {
		e.FramingTokens = saturatedSum(e.FramingTokens, 4)
		text(m.Name)
		text(m.ToolCallID)
		if err := content(m.Content); err != nil {
			return e, err
		}
		if m.ReasoningContent != nil {
			text(*m.ReasoningContent)
		}
		if m.Refusal != nil {
			text(*m.Refusal)
		}
		for _, call := range m.ToolCalls {
			e.FramingTokens = saturatedSum(e.FramingTokens, 8)
			text(call.ID)
			text(call.Function.Name)
			// Arguments are already a decoded string, not their escaped wire envelope.
			// Preserve non-JSON partial/opaque arguments conservatively as byte counts.
			e.StructuredBytes = saturatedSum(e.StructuredBytes, int64(len(call.Function.Arguments)))
		}
	}
	for _, tool := range r.Tools {
		e.FramingTokens = saturatedSum(e.FramingTokens, 8)
		text(tool.Function.Name)
		if tool.Function.Description != nil {
			text(*tool.Function.Description)
		}
		if err := structure(tool.Function.Parameters); err != nil {
			return e, err
		}
	}
	for _, key := range []string{"response_format", "tool_choice"} {
		if err := structure(r.Options[key]); err != nil {
			return e, err
		}
	}
	if raw := r.Options["max_completion_tokens"]; raw != nil {
		if json.Unmarshal(raw, &e.OutputReserve) != nil || e.OutputReserve <= 0 {
			return e, Fail("invalid_request", "cannot estimate output reserve")
		}
	}
	textTokens := e.TextBytes / 4
	if e.TextBytes%4 != 0 {
		textTokens++
	}
	e.InputTokens = saturatedSum(textTokens, e.StructuredBytes, e.ImageTokens, e.FramingTokens)
	e.TotalTokens = saturatedSum(e.InputTokens, e.OutputReserve)
	return e, nil
}
