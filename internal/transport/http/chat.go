package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"time"
)

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	execution := s.inference.Begin(r.Context(), w.Header().Get("X-Request-ID"))
	var finalErr error
	defer func() { execution.Finish(finalErr) }()
	fail := func(e error) { finalErr = e; writeError(w, e) }
	r.Body = http.MaxBytesReader(w, r.Body, s.limits.MaxBodyBytes)
	var req routing.Request
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&req); err != nil {
		var fieldError *routing.Error
		if errors.As(err, &fieldError) {
			fail(fieldError)
		} else {
			fail(routing.Fail("invalid_request", "invalid chat body"))
		}
		return
	}
	if d.Decode(new(any)) != io.EOF {
		fail(routing.Fail("invalid_request", "invalid chat body"))
		return
	}
	plan, e := execution.Plan(req, nil)
	if e != nil {
		fail(e)
		return
	}
	id := w.Header().Get("X-Request-ID")
	if !req.Stream {
		ctx, cancel := context.WithTimeout(r.Context(), s.limits.FirstEventTimeout)
		defer cancel()
		result, e := s.executor.Complete(ctx, plan, req)
		e = contextError(ctx, e)
		if e != nil {
			fail(e)
			return
		}
		http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.limits.IdleTimeout))
		finalErr = WriteCompletion(w, result, plan.ModelID, id)
		return
	}
	ctx, stop := context.WithCancelCause(r.Context())
	defer stop(context.Canceled)
	timer := time.AfterFunc(s.limits.FirstEventTimeout, func() { stop(context.DeadlineExceeded) })
	defer timer.Stop()
	stream, e := s.executor.Stream(ctx, plan, req)
	if e != nil {
		fail(contextError(ctx, e))
		return
	}
	buffer := bufferStream(ctx, stream, s.limits.StreamBuffer)
	defer buffer.Close()
	// Headers are committed only once a valid event is available. Errors after
	// that point use an SSE error object and never a successful DONE marker.
	var first routing.Event
	for {
		first, e = buffer.Next(ctx)
		if e != nil {
			fail(contextError(ctx, e))
			return
		}
		if validEvent(first) {
			break
		}
	}
	if !timer.Stop() {
		fail(routing.Fail("timeout", "first event timed out"))
		return
	}
	finalErr = writeStream(ctx, w, &prependStream{first: &first, rest: buffer}, plan.ModelID, id, s.limits.IdleTimeout)
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
