package httptransport

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"time"
)

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	execution := s.inference.Begin(r.Context(), w.Header().Get("X-Request-ID"))
	var finalErr error
	defer func() { execution.Finish(finalErr) }()
	fail := func(e error) { finalErr = e; writeError(w, e) }
	r.Body = http.MaxBytesReader(w, r.Body, s.limits.MaxBodyBytes)
	req, e := decodeChatRequest(r.Body)
	if e != nil {
		fail(e)
		return
	}
	id := w.Header().Get("X-Request-ID")
	started := false
	finalErr = executeChat(r.Context(), execution, s.executor, req, s.limits.FirstEventTimeout, s.limits.StreamBuffer, chatOutput{
		complete: func(result routing.Completion, plan routing.Plan) error {
			started = true
			http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.limits.IdleTimeout))
			return WriteCompletion(w, result, plan.ModelID, id)
		},
		stream: func(ctx context.Context, stream routing.EventStream, plan routing.Plan) error {
			started = true
			return writeStream(ctx, w, stream, plan.ModelID, id, s.limits.IdleTimeout)
		},
	})
	if finalErr != nil && !started {
		writeError(w, finalErr)
	}

}
func completionBody(result routing.Completion, model, kind string) map[string]any {
	b := map[string]any{"id": result.ID, "object": kind, "created": result.Created, "model": model, "choices": result.Choices}
	if result.Usage != nil {
		u := result.Usage
		usage := map[string]any{"prompt_tokens": u.InputTokens, "completion_tokens": u.OutputTokens, "total_tokens": u.TotalTokens}
		if u.InputDetails != nil {
			usage["prompt_tokens_details"] = u.InputDetails
		}
		if u.OutputDetails != nil {
			usage["completion_tokens_details"] = u.OutputDetails
		}
		b["usage"] = usage
	}
	return b
}
func WriteCompletion(w http.ResponseWriter, result routing.Completion, modelID, requestID string) error {
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(completionBody(result, modelID, "chat.completion"))
}
