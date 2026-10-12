package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerutils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func (p *PreparedMessages) chatRequest() (routing.Request, error) {
	body := p.body
	if err := rejectNativeChatFields(body); err != nil {
		return routing.Request{}, err
	}
	// The pinned SDK's Sonic decoders differ by architecture when numbers
	// overflow float64. Check representability consistently before conversion.
	// This decoded value is never used for output: raw tool numbers retain
	// their precision in the SDK's RawMessage fields.
	var numericBounds any
	if json.Unmarshal(body, &numericBounds) != nil {
		return routing.Request{}, routing.Fail("unsupported_request", "Messages data cannot be represented by the Chat adapter")
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
		return routing.Request{}, routing.Fail("unsupported_request", "Messages data cannot be represented by the Chat adapter")
	}
	if in.Thinking != nil && in.Thinking.Type == "enabled" {
		return routing.Request{}, routing.Fail("unsupported_request", "thinking.budget_tokens cannot be enforced by the configured Chat adapter; use adaptive thinking without a token budget")
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
				return routing.Request{}, routing.Fail("unsupported_request", fmt.Sprintf("messages[%d].content: assistant text after tool use cannot be represented by the Chat adapter", i))
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
						return routing.Request{}, reject("unsupported_request", "assistant thinking must precede text and tool use")
					}
					if block.Signature != nil && *block.Signature != "" {
						// Bifrost embeds a Responses item ID for round trips. An
						// empty payload is bookkeeping, not a Claude signature.
						_, payload, marker := providerutils.ExtractReasoningItemID(*block.Signature)
						if !marker || payload != "" {
							return routing.Request{}, reject("unsupported_request", "signed thinking history is not supported by the Chat adapter")
						}
					}
				case "text":
					if stage > 1 {
						return routing.Request{}, reject("unsupported_request", "assistant text after tool use cannot be represented by the Chat adapter")
					}
					stage = 1
				case "tool_use":
					stage = 2
				default:
					return routing.Request{}, reject("unsupported_request", "unsupported assistant content block")
				}
			} else {
				switch typ {
				case "tool_result":
					if stage > 0 {
						return routing.Request{}, reject("unsupported_request", "tool results must precede user content")
					}
					if block.IsError != nil && *block.IsError {
						return routing.Request{}, reject("unsupported_request", "tool_result.is_error is not supported by the Chat adapter")
					}
				case "text", "image":
					stage = 1
				default:
					return routing.Request{}, reject("unsupported_request", "unsupported user content block")
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
		return routing.Request{}, routing.Fail("unsupported_request", "cannot convert Messages request")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(wire, &fields) != nil {
		return routing.Request{}, routing.Fail("internal_error", "cannot prepare Messages request")
	}
	var messages []routing.Message
	if json.Unmarshal(fields["messages"], &messages) != nil {
		return routing.Request{}, routing.Fail("internal_error", "cannot prepare Messages history")
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
		return routing.Request{}, routing.Fail("internal_error", "cannot prepare Messages projection")
	}
	if err := routing.ValidateRequest(projection); err != nil {
		return routing.Request{}, routing.Fail("unsupported_request", "Messages history cannot be represented by the Chat adapter")
	}

	return projection, nil
}
