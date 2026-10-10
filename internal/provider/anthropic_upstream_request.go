package provider

import (
	"encoding/json"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"reflect"
	"strings"
)

func nativeUnsupported(field string) error {
	return routing.Fail("unsupported_request", "Anthropic adapter cannot preserve field: "+field)
}
func prepareAnthropicUpstream(ctx *schemas.BifrostContext, t routing.Target, r routing.Request) (*schemas.BifrostChatRequest, error) {
	allowed := map[string]bool{"max_completion_tokens": true, "temperature": true, "top_p": true, "stop": true, "tool_choice": true, "parallel_tool_calls": true, "response_format": true, "stream_options": true, "chat_template_kwargs": true, "reasoning_effort": true}
	for k := range r.Options {
		if !allowed[k] {
			return nil, nativeUnsupported(k)
		}
	}
	thinking := ""
	var knobs struct {
		Enable *bool `json:"enable_thinking"`
	}
	json.Unmarshal(r.Options["chat_template_kwargs"], &knobs)
	if knobs.Enable != nil {
		thinking = "disabled"
		if *knobs.Enable {
			thinking = "adaptive"
		}
	}
	var effort string
	json.Unmarshal(r.Options["reasoning_effort"], &effort)
	switch effort {
	case "":
	case "none":
		if thinking == "adaptive" {
			return nil, nativeUnsupported("reasoning_effort")
		}
		thinking = "disabled"
	case "low", "medium", "high", "max":
		if thinking == "disabled" {
			return nil, nativeUnsupported("reasoning_effort")
		}
	default:
		return nil, nativeUnsupported("reasoning_effort")
	}
	for _, m := range r.Messages {
		if m.Refusal != nil || m.Name != "" {
			return nil, nativeUnsupported("messages")
		}
	}
	in, e := encodeOpenAI(ctx, t, r)
	if e != nil {
		return nil, routing.Fail("internal_error", "cannot prepare generation request")
	}
	in.Provider = schemas.Anthropic
	// Chat clients replay plain thinking as reasoning_content. The SDK's
	// Anthropic serializer consumes structured details, not the summary field.
	// Preserve the text without manufacturing a native integrity signature.
	for i := range in.Input {
		m := &in.Input[i]
		if m.ChatAssistantMessage != nil && m.Reasoning != nil && *m.Reasoning != "" {
			m.ReasoningDetails = []schemas.ChatReasoningDetails{{Type: schemas.BifrostReasoningDetailsTypeText, Text: m.Reasoning}}
		}
	}
	in.Params.ExtraParams = map[string]any{}
	if in.Params.MaxCompletionTokens == nil {
		in.Params.MaxCompletionTokens = schemas.Ptr(65536)
	}
	if thinking != "" {
		in.Params.ExtraParams["thinking"] = map[string]any{"type": thinking}
	}
	if effort != "" && effort != "none" {
		in.Params.ExtraParams["output_config"] = map[string]any{"effort": effort}
	}
	body, fail := anthropic.BuildAnthropicChatRequestBody(ctx, in, anthropic.AnthropicRequestBuildConfig{Provider: schemas.Anthropic, IsStreaming: r.Stream})
	if fail != nil {
		return nil, nativeUnsupported("request conversion")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return nil, routing.Fail("internal_error", "invalid prepared generation request")
	}
	// The final Chat builder strips unsigned thinking for Claude. Compatible
	// native providers can require it on replay. Reuse the SDK conversion but
	// send these checked bytes through its public passthrough transport.
	hasReasoning := false
	for _, m := range r.Messages {
		hasReasoning = hasReasoning || (m.ReasoningContent != nil && *m.ReasoningContent != "")
	}
	if hasReasoning {
		native, err := anthropic.ToAnthropicChatRequest(ctx, in)
		if err != nil {
			return nil, nativeUnsupported("messages")
		}
		messages, err := json.Marshal(native.Messages)
		if err != nil {
			return nil, nativeUnsupported("messages")
		}
		fields["messages"] = messages
		body, err = json.Marshal(fields)
		if err != nil {
			return nil, routing.Fail("internal_error", "invalid prepared generation request")
		}
	}
	wire, _ := decoded(body).(map[string]any)
	if e = checkNativeWire(r, wire, thinking, effort); e != nil {
		return nil, e
	}
	in.RawRequestBody = body
	return in, nil
}
func checkNativeWire(r routing.Request, w map[string]any, thinking, effort string) error {
	limit := json.Number("65536")
	if v := r.Options["max_completion_tokens"]; v != nil {
		limit = decoded(v).(json.Number)
	}
	if w["max_tokens"] != limit {
		return nativeUnsupported("max_completion_tokens")
	}
	for _, k := range []string{"temperature", "top_p"} {
		if v := r.Options[k]; v != nil && !reflect.DeepEqual(decoded(v), w[k]) {
			return nativeUnsupported(k)
		}
	}
	if v := r.Options["stop"]; v != nil {
		want := decoded(v)
		if s, ok := want.(string); ok {
			want = []any{s}
		}
		if !reflect.DeepEqual(want, w["stop_sequences"]) {
			return nativeUnsupported("stop")
		}
	}
	if thinking != "" && !reflect.DeepEqual(w["thinking"], map[string]any{"type": thinking}) {
		return nativeUnsupported("thinking")
	}
	if effort != "" && effort != "none" {
		o, _ := w["output_config"].(map[string]any)
		if o["effort"] != effort {
			return nativeUnsupported("reasoning_effort")
		}
	}
	if thinking == "" && w["thinking"] != nil {
		return nativeUnsupported("thinking")
	}
	if v := r.Options["response_format"]; v != nil {
		f, _ := decoded(v).(map[string]any)
		switch f["type"] {
		case "text":
		case "json_schema":
			j, _ := f["json_schema"].(map[string]any)
			// The native format has no equivalent for this model instruction.
			if description, _ := j["description"].(string); description != "" {
				return nativeUnsupported("response_format.json_schema.description")
			}
			o, _ := w["output_config"].(map[string]any)
			format, _ := o["format"].(map[string]any)
			if format["type"] != "json_schema" || !reflect.DeepEqual(format["schema"], j["schema"]) {
				return nativeUnsupported("response_format")
			}
		default:
			return nativeUnsupported("response_format")
		}
	}
	if e := checkNativeMessages(r, w); e != nil {
		return e
	}
	if len(r.Tools) > 0 {
		tools, _ := w["tools"].([]any)
		if len(tools) != len(r.Tools) {
			return nativeUnsupported("tools")
		}
		for i, t := range r.Tools {
			v, _ := tools[i].(map[string]any)
			if v["name"] != t.Function.Name || !nativeSchemaEqual(v["input_schema"], decoded(t.Function.Parameters)) {
				return nativeUnsupported("tools")
			}
			if t.Function.Description != nil && v["description"] != *t.Function.Description {
				return nativeUnsupported("tools.description")
			}
			if t.Function.Strict != nil && v["strict"] != *t.Function.Strict {
				return nativeUnsupported("tools.strict")
			}
		}
	}
	choice, _ := w["tool_choice"].(map[string]any)
	if v := r.Options["tool_choice"]; v != nil {
		want := decoded(v)
		typ := ""
		name := ""
		if s, ok := want.(string); ok {
			typ = s
			if s == "required" {
				typ = "any"
			}
		} else {
			obj, _ := want.(map[string]any)
			f, _ := obj["function"].(map[string]any)
			typ = "tool"
			name, _ = f["name"].(string)
		}
		if choice["type"] != typ || (name != "" && choice["name"] != name) {
			return nativeUnsupported("tool_choice")
		}
	}
	if v := r.Options["parallel_tool_calls"]; v != nil {
		want, _ := decoded(v).(bool)
		disabled, _ := choice["disable_parallel_tool_use"].(bool)
		if choice["type"] != "none" && disabled != !want {
			return nativeUnsupported("parallel_tool_calls")
		}
	}
	return nil
}

// Compare semantic atoms independently of the SDK conversion. Adjacent same-role
// messages may merge, but text, images, call IDs/arguments and result order may not.
func checkNativeMessages(r routing.Request, w map[string]any) error {
	var expected, actual []any
	var systems []any
	ordinary := false
	atom := func(role string, v any) any { return []any{role, v} }
	for _, m := range r.Messages {
		if m.Role == "system" || m.Role == "developer" {
			if ordinary {
				return nativeUnsupported("messages.system order")
			}
			v := decoded(m.Content)
			s, ok := v.(string)
			if !ok {
				return nativeUnsupported("messages.system content")
			}
			systems = append(systems, s)
			continue
		}
		ordinary = true
		if m.Role == "tool" {
			expected = append(expected, atom("user", map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": decoded(m.Content)}))
			continue
		}
		if m.ReasoningContent != nil && *m.ReasoningContent != "" {
			expected = append(expected, atom(m.Role, map[string]any{"type": "thinking", "thinking": *m.ReasoningContent}))
		}
		content := decoded(m.Content)
		if text, ok := content.(string); ok && text != "" {
			expected = append(expected, atom(m.Role, map[string]any{"type": "text", "text": text}))
		}
		if parts, ok := content.([]any); ok {
			for _, part := range parts {
				p, _ := part.(map[string]any)
				switch p["type"] {
				case "text":
					expected = append(expected, atom(m.Role, p))
				case "image_url":
					image, _ := p["image_url"].(map[string]any)
					if image["detail"] != nil && image["detail"] != "auto" {
						return nativeUnsupported("messages.image_url.detail")
					}
					url, _ := image["url"].(string)
					source := map[string]any{"type": "url", "url": url}
					if strings.HasPrefix(url, "data:") {
						pieces := strings.SplitN(url, ",", 2)
						if len(pieces) != 2 {
							return nativeUnsupported("messages.image_url")
						}
						source = map[string]any{"type": "base64", "media_type": strings.TrimSuffix(strings.TrimPrefix(pieces[0], "data:"), ";base64"), "data": pieces[1]}
					}
					expected = append(expected, atom(m.Role, map[string]any{"type": "image", "source": source}))
				default:
					return nativeUnsupported("messages.content")
				}
			}
		}
		for _, c := range m.ToolCalls {
			args, _ := decoded([]byte(c.Function.Arguments)).(map[string]any)
			if args == nil {
				return nativeUnsupported("messages.tool_calls.arguments")
			}
			expected = append(expected, atom(m.Role, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Function.Name, "input": args}))
		}
	}
	var gotSystem []any
	switch v := w["system"].(type) {
	case string:
		gotSystem = append(gotSystem, v)
	case []any:
		for _, b := range v {
			m, _ := b.(map[string]any)
			if m["type"] != "text" {
				return nativeUnsupported("messages.system")
			}
			gotSystem = append(gotSystem, m["text"])
		}
	}
	if !reflect.DeepEqual(systems, gotSystem) {
		return nativeUnsupported("messages.system")
	}
	messages, _ := w["messages"].([]any)
	for _, v := range messages {
		m, _ := v.(map[string]any)
		role, _ := m["role"].(string)
		if text, ok := m["content"].(string); ok {
			if text != "" {
				actual = append(actual, atom(role, map[string]any{"type": "text", "text": text}))
			}
			continue
		}
		blocks, _ := m["content"].([]any)
		for _, b := range blocks {
			block, _ := b.(map[string]any)
			if block["type"] == "text" && block["text"] == "" {
				continue
			}
			if block["type"] == "tool_result" {
				if blocks, ok := block["content"].([]any); ok && len(blocks) == 1 {
					v, _ := blocks[0].(map[string]any)
					if v["type"] == "text" {
						block["content"] = v["text"]
					}
				}
			}
			actual = append(actual, atom(role, block))
		}
	}
	if !reflect.DeepEqual(expected, actual) {
		return nativeUnsupported("messages")
	}
	return nil
}

// An empty properties object constrains no properties, just like omission.
func nativeSchemaEqual(a, b any) bool {
	normalize := func(v any) any {
		if m, ok := v.(map[string]any); ok {
			if props, ok := m["properties"].(map[string]any); ok && len(props) == 0 {
				delete(m, "properties")
			}
		}
		return v
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
