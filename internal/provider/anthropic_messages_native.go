package provider

import (
	"encoding/json"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func nativeMessagesTransportRequest(model string, body json.RawMessage) *schemas.BifrostPassthroughRequest {
	return &schemas.BifrostPassthroughRequest{Model: model, Method: "POST", Path: "/messages", Body: body, SafeHeaders: map[string]string{"Content-Type": "application/json"}}
}
func completeNativeMessages(ctx *schemas.BifrostContext, client *bifrost.Bifrost, target routing.Target, prepared *PreparedMessages) (json.RawMessage, error) {
	body, err := prepared.nativeBody(target)
	if err != nil {
		return nil, err
	}
	out, failure := client.Passthrough(ctx, schemas.Anthropic, nativeMessagesTransportRequest(target.Model.UpstreamName, body))
	if ctx.Err() != nil || failure != nil {
		return nil, anthropicUpstreamError(ctx, failure)
	}
	if out == nil {
		return nil, nativeError(502)
	}
	if out.StatusCode != 200 {
		err := nativeReportedError(out.StatusCode, out.Body, upstreamSecret(ctx)).(*routing.UpstreamError)
		addUpstreamRequestID(ctx, err.Details, out.Headers)
		return nil, err
	}
	return validateNativeMessagesResponse(out.Body, target, prepared)
}
