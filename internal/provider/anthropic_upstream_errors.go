package provider

import (
	"context"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func nativeError(status int) error {
	code, message := "upstream_error", "generation provider failed"
	switch status {
	case 400, 413, 422:
		code, message = "invalid_request", "upstream rejected generation request"
	case 404:
		code, message = "not_found", "upstream model unavailable"
	case 429:
		code, message = "rate_limit_exceeded", "upstream rate limit exceeded"
	case 529:
		code, message = "upstream_overloaded", "upstream overloaded"
	case 504:
		code, message = "timeout", "upstream request timed out"
	case 499:
		code, message = "cancelled", "request cancelled"
	default:
		if status < 500 || status > 599 {
			status = 502
		}
	}
	return &routing.UpstreamError{Status: status, Body: map[string]any{"message": message, "type": code, "code": code, "param": nil}}
}
func anthropicUpstreamError(ctx context.Context, fail *schemas.BifrostError) error {
	if ctx.Err() == context.DeadlineExceeded {
		return nativeError(504)
	}
	if ctx.Err() != nil {
		return nativeError(499)
	}
	status := 502
	if fail != nil && fail.StatusCode != nil {
		status = *fail.StatusCode
	}
	return nativeError(status)
}
