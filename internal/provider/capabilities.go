package provider

import (
	"context"
	"encoding/json"
	"github.com/maximhq/bifrost/core/providers/openai"
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
	ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
	defer ctx.Cancel()
	req, e := encode(ctx, target, r)
	if e != nil {
		return routing.Fail("unsupported_request", "cannot encode request")
	}
	converted := openai.ToOpenAIChatRequest(ctx, req)
	after, _ := json.Marshal(converted)
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
