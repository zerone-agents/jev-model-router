package provider

import (
	"context"
	"encoding/json"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerutils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"reflect"
	"time"
)

func TestRequest() routing.Request {
	return routing.Request{Model: "probe", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"Reply OK."`)}}, Options: map[string]json.RawMessage{"max_completion_tokens": json.RawMessage(`16`)}}
}
func encode(ctx *schemas.BifrostContext, target routing.Target, r routing.Request) (*schemas.BifrostChatRequest, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	var in openai.OpenAIChatRequest
	if e = json.Unmarshal(b, &in); e != nil {
		return nil, e
	}
	req := in.ToBifrostChatRequest(ctx)
	req.Provider = schemas.OpenAI
	req.Model = target.Model.UpstreamName
	req.Fallbacks = nil
	// The current adapter targets OpenAI-compatible endpoints. Preserve the
	// validated wire controls rather than applying OpenAI model-name heuristics
	// to third-party models. Only these allowlisted fields can enter ExtraParams.
	req.Params.Reasoning = nil
	req.Params.ExtraParams = map[string]interface{}{}
	for _, key := range []string{"chat_template_kwargs", "reasoning_effort"} {
		if raw := r.Options[key]; raw != nil {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			req.Params.ExtraParams[key] = value
		}
	}
	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	return req, nil
}
func Check(target routing.Target, r routing.Request) error {
	if e := routing.ValidateRequest(r); e != nil {
		return e
	}
	if routing.HasImages(r) && !target.Model.Capabilities.Images {
		return routing.Fail("unsupported_request", "model does not support images")
	}
	if routing.RequiresTools(r) && !target.Model.Capabilities.Tools {
		return routing.Fail("unsupported_request", "model does not support tools")
	}
	if routing.RequiresStructuredOutput(r) && !target.Model.Capabilities.StructuredOutput {
		return routing.Fail("unsupported_request", "model does not support structured output")
	}
	return checkWire(target, r)
}

func checkWire(target routing.Target, r routing.Request) error {
	ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
	defer ctx.Cancel()
	req, e := encode(ctx, target, r)
	if e != nil {
		return routing.Fail("unsupported_request", "cannot encode request")
	}
	// Use the same final serialization and extension merge as the actual send.
	after, failure := providerutils.CheckContextAndGetRequestBody(ctx, req, func() (providerutils.RequestBodyWithExtraParams, error) {
		return openai.ToOpenAIChatRequest(ctx, req), nil
	})
	if failure != nil {
		return routing.Fail("unsupported_request", "cannot encode request")
	}
	before, _ := json.Marshal(r)
	var a, b map[string]any
	json.Unmarshal(before, &a)
	json.Unmarshal(after, &b)
	normalizeToolHistory(a)
	normalizeToolHistory(b)
	delete(a, "model")
	delete(a, "stream")
	for k, v := range a {
		if !reflect.DeepEqual(v, b[k]) {
			return routing.Fail("unsupported_request", "selected adapter would change request field: "+k)
		}
	}
	return nil
}

// The pinned adapter adds stream indexes to historical calls and omits null
// assistant content. These representations carry the same tool-turn semantics.
// All call IDs, argument strings, ordering and tool result contents still compare.
func normalizeToolHistory(request map[string]any) {
	messages, _ := request["messages"].([]any)
	for _, v := range messages {
		m, ok := v.(map[string]any)
		if !ok || m["role"] != "assistant" {
			continue
		}
		calls, _ := m["tool_calls"].([]any)
		if len(calls) == 0 {
			continue
		}
		if m["content"] == nil {
			delete(m, "content")
		}
		for _, c := range calls {
			if call, ok := c.(map[string]any); ok {
				delete(call, "index")
			}
		}
	}
}

// PrepareCheck is scoped to one Plan call, after ValidateRequest and per-model
// capability checks. The pinned OpenAI adapter converts message history without
// consulting model capabilities; model-dependent rewrites affect parameters.
// Check the full history once and retain per-target parameter checks. No result
// is shared across requests. Check remains the standalone full validation path.
func PrepareCheck(r routing.Request) func(routing.Target) error {
	history := routing.Request{Model: r.Model, Messages: r.Messages}
	parameters := r
	parameters.Messages = []routing.Message{{Role: "user", Content: json.RawMessage(`"compatibility check"`)}}
	checked := false
	var historyError error
	return func(target routing.Target) error {
		if !checked {
			historyError = checkWire(target, history)
			checked = true
		}
		if historyError != nil {
			return historyError
		}
		return checkWire(target, parameters)
	}
}
