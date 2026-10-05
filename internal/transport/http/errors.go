package httptransport

import (
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
)

func errorBody(e error) map[string]any {
	var upstream *routing.UpstreamError
	if errors.As(e, &upstream) {
		return map[string]any{"error": upstream.Body}
	}
	known := management.Failure(e).Error
	return map[string]any{"error": map[string]any{"message": known.Message, "type": known.Code, "code": known.Code, "param": nil}}
}
func writeError(w http.ResponseWriter, e error) {
	known := management.Failure(e).Error
	w.Header().Set("Content-Type", "application/json")
	status := contracts.ErrorMapping(known.Code).HTTP
	var upstream *routing.UpstreamError
	if errors.As(e, &upstream) && upstream.Status >= 400 && upstream.Status <= 599 {
		status = upstream.Status
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorBody(e))
}
