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
		body := make(map[string]any, len(upstream.Body)+1)
		for k, v := range upstream.Body {
			body[k] = v
		}
		body["diagnostic"] = routing.Diagnose(e)
		return map[string]any{"error": body}
	}
	known := management.Failure(e).Error
	body := map[string]any{"message": known.Message, "type": known.Code, "code": known.Code, "param": nil}
	if len(known.HealthExclusions) > 0 {
		body["health_exclusions"] = known.HealthExclusions
		if known.HealthExclusionsTruncated {
			body["health_exclusions_truncated"] = true
		}
	}
	var attempted *routing.AttemptError
	if errors.As(e, &attempted) {
		body["diagnostic"] = routing.Diagnose(e)
	}
	return map[string]any{"error": body}
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
