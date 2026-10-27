package provider

import (
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"math/big"
)

// Projection is decision-only. Tool arguments/results and opaque native fields
// never enter it; execution always rebuilds from the immutable native body.
func (p *PreparedMessages) Projection() routing.Request {
	var doc messagesDocument
	_ = json.Unmarshal(p.body, &doc)
	r := routing.Request{Model: doc.Model, Stream: doc.Stream, Options: map[string]json.RawMessage{"max_completion_tokens": json.RawMessage(doc.MaxTokens)}}
	limit, _ := new(big.Rat).SetString(string(doc.MaxTokens))
	r.Options["max_completion_tokens"] = json.RawMessage(limit.Num().String())
	image := json.RawMessage(`{"type":"image_url","image_url":{"url":"https://image.invalid/"}}`)
	text := func(s string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"type": "text", "text": s})
		return b
	}
	content := func(raw json.RawMessage) json.RawMessage {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return append(json.RawMessage(nil), raw...)
		}
		parts := []json.RawMessage{}
		for _, b := range inputBlocks(raw) {
			switch b.Type {
			case "text":
				parts = append(parts, text(b.Text))
			case "image":
				parts = append(parts, image)
			}
		}
		if len(parts) == 0 {
			return json.RawMessage(`""`)
		}
		v, _ := json.Marshal(parts)
		return v
	}
	if len(doc.System) > 0 {
		r.Messages = append(r.Messages, routing.Message{Role: "system", Content: content(doc.System)})
	}
	for _, m := range doc.Messages {
		// Results precede user content only in this decision representation.
		for _, b := range inputBlocks(m.Content) {
			if b.Type == "tool_result" {
				r.Messages = append(r.Messages, routing.Message{Role: "tool", ToolCallID: b.ToolUseID, Content: json.RawMessage(`""`)})
				for _, result := range inputBlocks(b.Content) {
					if result.Type == "image" {
						r.Messages[len(r.Messages)-1].Content = json.RawMessage(`[` + string(image) + `]`)
					}
				}
			}
		}
		msg := routing.Message{Role: m.Role, Content: content(m.Content)}
		for _, b := range inputBlocks(m.Content) {
			if b.Type == "tool_use" {
				msg.ToolCalls = append(msg.ToolCalls, routing.ToolCall{ID: b.ID, Type: "function", Function: routing.CallFunction{Name: b.Name, Arguments: "{}"}})
			}
		}
		// Tool-only user messages need no additional empty user turn.
		if m.Role == "user" && string(msg.Content) == `""` && len(inputBlocks(m.Content)) > 0 {
			continue
		}
		r.Messages = append(r.Messages, msg)
	}
	for _, raw := range doc.Tools {
		var tool struct {
			Name        string          `json:"name"`
			Description *string         `json:"description"`
			Schema      json.RawMessage `json:"input_schema"`
		}
		_ = json.Unmarshal(raw, &tool)
		r.Tools = append(r.Tools, routing.Tool{Type: "function", Function: routing.Function{Name: tool.Name, Description: tool.Description, Parameters: tool.Schema}})
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(p.body, &fields)
	var output struct {
		Format json.RawMessage `json:"format"`
		Effort json.RawMessage `json:"effort"`
	}
	_ = json.Unmarshal(fields["output_config"], &output)
	if len(output.Format) > 0 {
		r.Options["response_format"] = json.RawMessage(`{"type":"json_object"}`)
	}
	if output.Effort != nil {
		r.Options["reasoning_effort"] = append(json.RawMessage(nil), output.Effort...)
	}
	if doc.Thinking != nil {
		if doc.Thinking.Type == "disabled" || doc.Thinking.Budget != "" {
			r.AutoEffortBlocked = true
		}
	}
	return r
}
