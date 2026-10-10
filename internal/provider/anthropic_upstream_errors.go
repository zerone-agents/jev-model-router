package provider

import (
	"context"
	"encoding/json"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func nativeError(status int) error {
	code, message := "upstream_error", "generation provider failed"
	switch status {
	case 401, 402, 403, 409:
	// Preserve the real status; consumers use the shared safe diagnostic.
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
		if status < 400 || status > 599 {
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
	if fail != nil {
		err := nativeReportedError(status, fail.ExtraFields.RawResponse, upstreamSecret(ctx)).(*routing.UpstreamError)
		addUpstreamRequestID(ctx, err.Details, nil)
		return err
	}
	return nativeError(status)
}

// SSE carries an error type instead of an HTTP failure status. Resolve the
// documented status, then apply the same safe policy as HTTP errors.
func nativeEventError(typ string) error {
	status := 502
	switch typ {
	case "invalid_request_error":
		status = 400
	case "authentication_error":
		status = 401
	case "billing_error":
		status = 402
	case "permission_error":
		status = 403
	case "not_found_error":
		status = 404
	case "conflict_error":
		status = 409
	case "request_too_large":
		status = 413
	case "rate_limit_error":
		status = 429
	case "api_error":
		status = 500
	case "timeout_error":
		status = 504
	case "overloaded_error":
		status = 529
	}
	err := nativeError(status).(*routing.UpstreamError)
	err.ReportedCode = routing.ReportedErrorCode(map[string]any{"type": typ})
	return err
}

// Inspect only recognized codes in the actual envelope, never SDK messages.
func nativeReportedError(status int, raw any, secrets ...string) error {
	err := nativeError(status).(*routing.UpstreamError)
	err.Details = routing.SanitizeUpstream(raw, secrets...)
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		b, _ = json.Marshal(v)
	}
	if len(b) > 65536 {
		return err
	}
	var envelope struct {
		Error map[string]any `json:"error"`
	}
	if json.Unmarshal(b, &envelope) != nil {
		return err
	}
	err.ReportedCode = routing.ReportedErrorCode(envelope.Error)
	return err
}
