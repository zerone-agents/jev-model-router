package provider

import (
	"bytes"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"math"
	"math/big"
)

func nativeSum(values ...int64) int64 {
	var total int64
	for _, v := range values {
		if v > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += v
	}
	return total
}
func (p *PreparedMessages) Estimate(_ routing.Target) (routing.ContextEstimate, bool, error) {
	var doc messagesDocument
	_ = json.Unmarshal(p.body, &doc)
	limit, _ := new(big.Rat).SetString(string(doc.MaxTokens))
	e := routing.ContextEstimate{Method: "native_messages_bytes_v1", OutputReserve: limit.Num().Int64()}
	text := func(s string) { e.TextBytes = nativeSum(e.TextBytes, int64(len(s))) }
	structure := func(raw json.RawMessage) {
		var compact bytes.Buffer
		_ = json.Compact(&compact, raw)
		e.StructuredBytes = nativeSum(e.StructuredBytes, int64(compact.Len()))
	}
	var content func(json.RawMessage)
	content = func(raw json.RawMessage) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			text(s)
			return
		}
		for _, b := range inputBlocks(raw) {
			switch b.Type {
			case "text":
				text(b.Text)
			case "image":
				e.ImageTokens = nativeSum(e.ImageTokens, 8192)
			case "thinking":
				text(b.Thinking)
				text(b.Signature)
			case "redacted_thinking":
				text(b.Data)
			case "tool_use":
				text(b.ID)
				text(b.Name)
				structure(b.Input)
				e.FramingTokens = nativeSum(e.FramingTokens, 8)
			case "tool_result":
				text(b.ToolUseID)
				content(b.Content)
			}
			if len(b.Cache) > 0 {
				structure(b.Cache)
			}
		}
	}
	if len(doc.System) > 0 {
		e.FramingTokens = nativeSum(e.FramingTokens, 4)
		content(doc.System)
	}
	for _, m := range doc.Messages {
		e.FramingTokens = nativeSum(e.FramingTokens, 4)
		content(m.Content)
	}
	for _, raw := range doc.Tools {
		e.FramingTokens = nativeSum(e.FramingTokens, 8)
		structure(raw)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(p.body, &fields)
	for _, key := range []string{"tool_choice", "output_config", "thinking", "metadata", "stop_sequences"} {
		if raw := fields[key]; len(raw) > 0 {
			structure(raw)
		}
	}
	e.InputTokens = nativeSum((e.TextBytes+3)/4, e.StructuredBytes, e.ImageTokens, e.FramingTokens)
	e.TotalTokens = nativeSum(e.InputTokens, e.OutputReserve)
	return e, false, nil
}
