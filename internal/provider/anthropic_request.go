package provider

import (
	"encoding/json"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// PreparedMessages owns immutable request bytes. Projection returns a fresh
// copy so planning and per-target checks cannot mutate another request.
type PreparedMessages struct {
	body    json.RawMessage
	stops   bool
	summary string
}

func (p *PreparedMessages) Projection() routing.Request {
	r, _ := p.chatRequest()
	return r
}
func (p *PreparedMessages) Check(t routing.Target) error {
	if t.Provider.EffectiveProtocol() == routing.ProtocolAnthropic {
		_, err := p.nativeBody(t)
		return err
	}
	r, err := p.chatRequest()
	if err != nil {
		return err
	}
	return Check(t, r)
}

// Anthropic combines consecutive turns of the same role. Bifrost splits blocks
// into turns; coalesce adjacent turns without reordering text/images/tool use.
func mergeChatMessages(input []routing.Message) []routing.Message {
	var out []routing.Message
	for _, m := range input {
		if m.Role == "assistant" && len(m.Content) == 0 && len(m.ToolCalls) == 0 {
			m.Content = json.RawMessage(`""`)
		}
		if len(out) == 0 || m.Role == "tool" || out[len(out)-1].Role != m.Role {
			out = append(out, m)
			continue
		}
		prev := &out[len(out)-1]
		prev.Content = mergeContent(prev.Content, m.Content)
		if m.ReasoningContent != nil {
			s := ""
			if prev.ReasoningContent != nil {
				s = *prev.ReasoningContent
			}
			s += *m.ReasoningContent
			prev.ReasoningContent = &s
		}
		prev.ToolCalls = append(prev.ToolCalls, m.ToolCalls...)
	}
	return out
}
func mergeContent(a, b json.RawMessage) json.RawMessage {
	if len(a) == 0 || string(a) == "null" {
		return b
	}
	if len(b) == 0 || string(b) == "null" {
		return a
	}
	var sa, sb string
	if json.Unmarshal(a, &sa) == nil && json.Unmarshal(b, &sb) == nil {
		raw, _ := json.Marshal(sa + sb)
		return raw
	}
	parts := func(raw json.RawMessage) []json.RawMessage {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if s == "" {
				return nil
			}
			v, _ := json.Marshal(map[string]string{"type": "text", "text": s})
			return []json.RawMessage{v}
		}
		var v []json.RawMessage
		_ = json.Unmarshal(raw, &v)
		return v
	}
	raw, _ := json.Marshal(append(parts(a), parts(b)...))
	return raw
}

func validMessagesFinish(reason *string, stops bool) error {
	if reason == nil {
		return routing.Fail("upstream_error", "generation ended without a supported stop reason")
	}
	switch *reason {
	case "length", "tool_calls":
		return nil
	case "stop":
		if !stops {
			return nil
		}
	}
	return routing.Fail("upstream_error", "upstream stop reason cannot be represented accurately")
}

func validateToolArguments(raw string) error {
	var value map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &value) != nil || value == nil {
		return routing.Fail("upstream_error", "upstream tool input is not a JSON object")
	}
	return nil
}

// Only the initial unknown usage and actual final usage are normalized. SDK
// accumulators can accept usage={} initially and update input_tokens at the end.
func encodeMessageEvent(e *anthropic.AnthropicStreamEvent, usage *routing.Usage) (MessageEvent, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return MessageEvent{}, routing.Fail("upstream_error", "cannot encode Messages event")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	delete(fields, "extra_fields")
	if string(e.Type) == "message_start" {
		var msg map[string]json.RawMessage
		_ = json.Unmarshal(fields["message"], &msg)
		delete(msg, "extra_fields")
		msg["usage"] = json.RawMessage(`{}`)
		fields["message"], _ = json.Marshal(msg)
	}
	if string(e.Type) == "message_delta" {
		u := publicMessagesUsage(usage)
		fields["usage"], _ = json.Marshal(u)
	}
	b, _ = json.Marshal(fields)
	return MessageEvent{Type: string(e.Type), Data: b, Terminal: string(e.Type) == "message_stop"}, nil
}

// Summary uses the original protocol messages, before conversion merges roles or removes tool-only users.
func (p *PreparedMessages) Summary() string { return p.summary }
