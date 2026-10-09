package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerutils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// PreparedMessages owns immutable request bytes. Projection returns a fresh
// copy so planning and per-target checks cannot mutate another request.
type PreparedMessages struct {
	body    json.RawMessage
	stops   bool
	summary string
}

func PrepareMessages(body json.RawMessage) (*PreparedMessages, error) {
	if !utf8.Valid(body) {
		return nil, routing.Fail("invalid_request", "Messages body must be valid UTF-8")
	}
	if contracts.Validate("messages", body) != nil {
		return nil, routing.Fail("invalid_request", "invalid or unsupported Messages field: "+contracts.MessagesInvalidField(body))
	}
	// JSON Schema integers include 1024.0 and 1.024e3. The pinned SDK's Go int
	// decoder needs a canonical representation, without rounding or clamping.
	var original map[string]json.RawMessage
	_ = json.Unmarshal(body, &original)
	var limit float64
	_ = json.Unmarshal(original["max_tokens"], &limit)
	original["max_tokens"], _ = json.Marshal(int64(limit))
	body, _ = json.Marshal(original)
	var in anthropic.AnthropicMessageRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, routing.Fail("invalid_request", "invalid Messages body")
	}
	if in.Thinking != nil && in.Thinking.Type == "enabled" {
		return nil, routing.Fail("unsupported_request", "thinking.budget_tokens cannot be enforced by the configured Chat adapter; use adaptive thinking without a token budget")
	}
	// Reject block orders that Chat cannot represent rather than regrouping them
	// into a materially different assistant turn.
	stage := 0
	lastRole := ""
	for i := range in.Messages {
		m := &in.Messages[i]
		if string(m.Role) != lastRole {
			stage = 0
			lastRole = string(m.Role)
		}
		if m.Content.ContentStr != nil {
			if m.Role == "assistant" && stage > 1 {
				return nil, routing.Fail("unsupported_request", fmt.Sprintf("messages[%d].content: assistant text after tool use cannot be represented by the Chat adapter", i))
			}
			stage = 1
		}
		onlyThinking := len(m.Content.ContentBlocks) > 0
		for j, block := range m.Content.ContentBlocks {
			reject := func(code, message string) error {
				return routing.Fail(code, fmt.Sprintf("messages[%d].content[%d]: %s", i, j, message))
			}
			typ := string(block.Type)
			if typ != "thinking" {
				onlyThinking = false
			}
			if m.Role == "assistant" {
				switch typ {
				case "thinking":
					if stage > 0 {
						return nil, reject("unsupported_request", "assistant thinking must precede text and tool use")
					}
					if block.Signature != nil && *block.Signature != "" {
						// Bifrost embeds a Responses item ID for round trips. An
						// empty payload is bookkeeping, not a Claude signature.
						_, payload, marker := providerutils.ExtractReasoningItemID(*block.Signature)
						if !marker || payload != "" {
							return nil, reject("unsupported_request", "signed thinking history is not supported by the Chat adapter")
						}
					}
				case "text":
					if stage > 1 {
						return nil, reject("unsupported_request", "assistant text after tool use cannot be represented by the Chat adapter")
					}
					stage = 1
				case "tool_use":
					stage = 2
				default:
					return nil, reject("invalid_request", "unsupported assistant content block")
				}
			} else {
				switch typ {
				case "tool_result":
					if stage > 0 {
						return nil, reject("invalid_request", "tool results must precede user content")
					}
					if block.IsError != nil && *block.IsError {
						return nil, reject("unsupported_request", "tool_result.is_error is not supported by the Chat adapter")
					}
				case "text", "image":
					stage = 1
				default:
					return nil, reject("invalid_request", "unsupported user content block")
				}
			}
		}
		if onlyThinking {
			// Responses->Chat otherwise buffers this history until a future
			// assistant text/tool item, losing it across a following user turn.
			// An empty text carrier preserves the assistant turn and adds no text.
			m.Content.ContentBlocks = append(m.Content.ContentBlocks, anthropic.AnthropicContentBlock{Type: anthropic.AnthropicContentBlockTypeText, Text: schemas.Ptr("")})
		}
	}
	c := schemas.NewBifrostContext(context.Background(), time.Time{})
	defer c.Cancel()
	r := in.ToBifrostResponsesRequest(c).ToChatRequest()
	// OpenAI compatibility endpoints use the existing explicit wire controls.
	// Bifrost's model-name heuristics must not rewrite effort or invent defaults.
	r.Provider, r.Model, r.Fallbacks = schemas.OpenAI, in.Model, nil
	r.Params.Reasoning = nil
	r.Params.ExtraParams = map[string]any{}
	if in.Thinking != nil {
		r.Params.ExtraParams["chat_template_kwargs"] = map[string]any{"enable_thinking": in.Thinking.Type != "disabled"}
	}
	if in.OutputConfig != nil && in.OutputConfig.Effort != nil {
		r.Params.ExtraParams["reasoning_effort"] = *in.OutputConfig.Effort
	}
	if in.StopSequences != nil {
		r.Params.ExtraParams["stop"] = in.StopSequences
	}
	if in.ToolChoice != nil && in.ToolChoice.DisableParallelToolUse != nil {
		r.Params.ParallelToolCalls = schemas.Ptr(!*in.ToolChoice.DisableParallelToolUse)
	}
	c.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	wire, failure := providerutils.CheckContextAndGetRequestBody(c, r, func() (providerutils.RequestBodyWithExtraParams, error) { return openai.ToOpenAIChatRequest(c, r), nil })
	if failure != nil {
		return nil, routing.Fail("unsupported_request", "cannot convert Messages request")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(wire, &fields) != nil {
		return nil, routing.Fail("internal_error", "cannot prepare Messages request")
	}
	var messages []routing.Message
	if json.Unmarshal(fields["messages"], &messages) != nil {
		return nil, routing.Fail("internal_error", "cannot prepare Messages history")
	}
	messages = mergeChatMessages(messages)
	// No native fields beyond this schema are accepted. Bifrost's extra history
	// indexes are representation metadata; serializing routing messages strips them.
	for i := range messages {
		for j := range messages[i].ToolCalls {
			messages[i].ToolCalls[j].Index = nil
		}
	}
	fields["messages"], _ = json.Marshal(messages)
	fields["model"], _ = json.Marshal(in.Model)
	if in.Stream != nil && *in.Stream {
		fields["stream"] = json.RawMessage(`true`)
		fields["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	canonical, _ := json.Marshal(fields)
	var projection routing.Request
	if json.Unmarshal(canonical, &projection) != nil {
		return nil, routing.Fail("internal_error", "cannot prepare Messages projection")
	}
	if err := routing.ValidateRequest(projection); err != nil {
		return nil, err
	}
	var source struct {
		Messages []routing.Message `json:"messages"`
	}
	_ = json.Unmarshal(body, &source)
	return &PreparedMessages{body: canonical, stops: len(in.StopSequences) > 0, summary: routing.RequestSummary(routing.Request{Messages: source.Messages})}, nil
}

func (p *PreparedMessages) Projection() routing.Request {
	var r routing.Request
	_ = json.Unmarshal(p.body, &r)
	return r
}
func (p *PreparedMessages) Check(t routing.Target) error {
	if t.Provider.EffectiveProtocol() == routing.ProtocolAnthropic {
		return routing.Fail("unsupported_request", "Messages with native Anthropic upstream is not yet supported")
	}
	return Check(t, p.Projection())
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
