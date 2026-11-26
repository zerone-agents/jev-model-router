// Package failurecapture collects bounded diagnostic evidence for failed requests.
package failurecapture

import (
	"bytes"
	"encoding/json"
)

// Projection describes a complete JSON segment without retaining arbitrary values.
type Projection struct {
	Fields        map[string]any `json:"fields,omitempty"`
	Omitted       bool           `json:"omitted,omitempty"`
	OmittedFields int            `json:"omitted_fields,omitempty"`
	Invalid       bool           `json:"invalid,omitempty"`
}

var eventTypes = []string{
	"message_start", "message_stop", "message_delta", "content_block_start",
	"content_block_delta", "content_block_stop", "ping", "error", "text",
	"thinking", "redacted_thinking", "tool_use", "server_tool_use", "tool_result",
	"text_delta", "thinking_delta", "signature_delta", "input_json_delta",
	"function", "mcp", "mcp_tool_use", "mcp_tool_result",
}

var stopReasons = []string{
	"end_turn", "max_tokens", "stop_sequence", "tool_use", "pause_turn",
	"stop", "length", "tool_calls", "content_filter", "refusal",
}

// ProjectJSON accepts only complete JSON objects. Unknown keys and arbitrary
// string values never enter the projected output.
func ProjectJSON(raw []byte) Projection {
	if !json.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return Projection{Omitted: true, Invalid: true}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Projection{Omitted: true, Invalid: true}
	}
	return projectFields(fields, 0)
}

func projectFields(fields map[string]json.RawMessage, depth int) Projection {
	if depth >= 8 {
		return Projection{Omitted: true}
	}
	result := Projection{Fields: make(map[string]any)}
	for key, value := range fields {
		switch key {
		case "type", "stop_reason", "finish_reason":
			var label string
			allowed := stopReasons
			if key == "type" {
				allowed = eventTypes
			}
			if json.Unmarshal(value, &label) == nil {
				for _, candidate := range allowed {
					if label == candidate {
						result.Fields[key] = candidate
						break
					}
				}
			}
		case "index", "max_tokens", "max_completion_tokens", "input_tokens",
			"output_tokens", "prompt_tokens", "completion_tokens", "total_tokens":
			var count *uint32
			if json.Unmarshal(value, &count) == nil && count != nil {
				result.Fields[key] = *count
			}
		case "stream":
			var enabled *bool
			if json.Unmarshal(value, &enabled) == nil && enabled != nil {
				result.Fields[key] = *enabled
			}
		case "delta", "content_block", "message", "usage", "error":
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) == nil && nested != nil {
				child := projectFields(nested, depth+1)
				result.Fields[key] = child
				result.Omitted = result.Omitted || child.Omitted
			}
		case "messages", "choices", "content", "tools", "tool_calls":
			var elements []map[string]json.RawMessage
			if json.Unmarshal(value, &elements) == nil && elements != nil && len(elements) <= 256 {
				children := make([]Projection, 0, len(elements))
				for _, element := range elements {
					child := projectFields(element, depth+1)
					children = append(children, child)
					result.Omitted = result.Omitted || child.Omitted
				}
				result.Fields[key] = children
			}
		}
		if _, kept := result.Fields[key]; !kept {
			result.OmittedFields++
			result.Omitted = true
		}
	}
	return result
}
