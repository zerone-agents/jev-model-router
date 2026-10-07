package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func messagesError(err error, id string) (int, map[string]any) {
	status, typ, message := 500, "api_error", "inference operation failed"
	var upstream *routing.UpstreamError
	if errors.Is(err, routing.ErrGenerationCapacity) {
		status, message = 503, "generation connection capacity reached"
	} else if errors.As(err, &upstream) {
		status = upstream.Status
		message = "generation provider rejected the request; check model and provider configuration"
		switch status {
		case 400, 405, 422:
			typ = "invalid_request_error"
		case 404:
			typ = "not_found_error"
		case 409:
			typ = "conflict_error"
		case 413:
			typ = "request_too_large"
		case 429:
			typ = "rate_limit_error"
		case 504:
			typ = "timeout_error"
		case 529:
			typ = "overloaded_error"
		default:
			typ = "api_error"
			if status < 500 || status > 599 {
				status = 502
			}
		}
	} else {
		known := management.Failure(err).Error
		switch known.Code {
		case "unauthorized":
			status, typ, message = 401, "authentication_error", "valid inference credentials required"
		case "forbidden":
			status, typ, message = 403, "permission_error", "inference credential required"
		case "invalid_request", "unsupported_request":
			status, typ, message = 400, "invalid_request_error", known.Message
		case "method_not_allowed":
			status, typ, message = 405, "invalid_request_error", "POST required"
		case "request_too_large":
			status, typ, message = 413, "request_too_large", "request body exceeds Router limit"
		case "not_found":
			status, typ, message = 404, "not_found_error", "model unavailable"
		case "no_candidates", "budget_exceeded":
			status, typ, message = 422, "invalid_request_error", known.Message
		case "config_missing", "storage_error":
			status = 503
			message = "inference configuration or storage unavailable"
		case "timeout":
			status, typ, message = 504, "timeout_error", "inference request timed out"
		case "cancelled":
			status, message = 499, "inference request cancelled"
		case "upstream_error", "invalid_decision":
			status, message = 502, "generation or decision provider failed"
		}
	}
	return status, map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": message}, "request_id": id}
}
func writeMessagesError(w http.ResponseWriter, err error, id string) {
	status, body := messagesError(err, id)
	w.Header().Set("request-id", id)
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Content-Type", "application/json")
	if status == 405 {
		w.Header().Set("Allow", "POST")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
