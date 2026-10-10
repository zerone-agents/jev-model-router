package httptransport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

type playgroundHTTP struct {
	sessions  *SessionHTTP
	admission *playground.Service
	inference *inference.Service
	executor  *routing.Executor
	limits    playground.Limits
}

func NewPlaygroundHandler(sessions *SessionHTTP, admission *playground.Service, inferenceService *inference.Service, executor *routing.Executor, limits playground.Limits) http.Handler {
	return &playgroundHTTP{sessions, admission, inferenceService, executor, limits}
}
func (p *playgroundHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-ID", rand.Text())
	status := r.URL.Path == "/admin/v1/playground" && r.Method == "GET"
	generate := r.URL.Path == "/admin/v1/playground/completions" && r.Method == "POST"
	if !status && !generate {
		playgroundError(w, routing.Fail("method_not_allowed", "unsupported playground endpoint"))
		return
	}
	id, e := p.sessions.AuthenticatePlayground(r)
	if e != nil {
		playgroundError(w, e)
		return
	}
	if status {
		q, e := p.admission.Status(r.Context(), id)
		if e != nil {
			playgroundError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"enabled": p.limits.Enabled, "limits": p.limits, "timeout_seconds": int(p.limits.Timeout / time.Second), "quota": q})
		return
	}
	if !p.limits.Enabled {
		playgroundError(w, routing.Fail("playground_unavailable", "playground is disabled"))
		return
	}
	req, e := p.parse(w, r)
	if e != nil {
		playgroundError(w, e)
		return
	}
	lease, e := p.admission.Acquire(r.Context(), id)
	if e != nil {
		playgroundError(w, e)
		return
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(r.Context(), p.limits.Timeout)
	defer cancel()
	// Bound response writes as well as upstream operations. Cancellation unblocks slow readers.
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	controller.SetWriteDeadline(deadline)
	stopWrite := context.AfterFunc(ctx, func() { controller.SetWriteDeadline(time.Now()) })
	defer stopWrite()
	execution := p.inference.Begin(ctx, w.Header().Get("X-Request-ID"))
	finalErr := routing.Fail("internal_error", "playground execution interrupted")
	defer func() { execution.Finish(finalErr) }()
	started := false
	finalErr = executeChat(ctx, execution, p.executor, req, p.limits.Timeout, 0, chatOutput{
		stream: func(streamCtx context.Context, stream routing.EventStream, _ routing.Plan) error {
			started = true
			return writePlaygroundStream(streamCtx, w, stream, execution.Metadata())
		},
	})
	if finalErr != nil && !started {
		playgroundError(w, finalErr)
	}

}
func (p *playgroundHTTP) parse(w http.ResponseWriter, r *http.Request) (routing.Request, error) {
	var req routing.Request
	r.Body = http.MaxBytesReader(w, r.Body, int64(p.limits.BodyBytes))
	b, e := io.ReadAll(r.Body)
	var maxBytes *http.MaxBytesError
	if errors.As(e, &maxBytes) {
		return req, routing.Fail("request_too_large", "playground body is too large")
	}
	if e != nil || !utf8.Valid(b) {
		return req, routing.Fail("invalid_request", "invalid playground request")
	}
	if contracts.Validate("playground", b) != nil {
		return req, routing.Fail("invalid_request", "invalid playground request")
	}
	if req, e = decodeChatRequest(bytes.NewReader(b)); e != nil {
		return req, routing.Fail("invalid_request", "invalid playground request")
	}
	if len(req.Messages) > p.limits.MaxMessages {
		return req, routing.Fail("request_too_large", "too many playground messages")
	}
	size := 0
	for _, m := range req.Messages {
		var text string
		json.Unmarshal(m.Content, &text)
		size += len(text)
		if m.ReasoningContent != nil {
			size += len(*m.ReasoningContent)
		}
	}
	if size > p.limits.InputBytes {
		return req, routing.Fail("request_too_large", "playground conversation is too large")
	}
	req = routing.Request{Model: req.Model, Messages: req.Messages, Stream: true}
	req.Options = map[string]json.RawMessage{"max_completion_tokens": json.RawMessage(strconv.Itoa(p.limits.OutputTokens)), "stream_options": json.RawMessage(`{"include_usage":true}`)}
	return req, routing.ValidateRequest(req)
}
func playgroundFailure(e error) *routing.Error {
	var known *routing.Error
	if errors.As(e, &known) {
		return known
	}
	var upstream *routing.UpstreamError
	if errors.As(e, &upstream) {
		return &routing.Error{Code: "upstream_error", Message: "generation provider failed"}
	}
	return &routing.Error{Code: "internal_error", Message: "playground unavailable"}
}
func playgroundError(w http.ResponseWriter, e error) {
	status := 500
	body := map[string]any{}
	var limit *playground.LimitError
	if errors.As(e, &limit) {
		seconds := max(1, int(math.Ceil(limit.RetryAfter.Seconds())))
		status = 429
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		body["error"] = map[string]any{"code": "playground_rate_limited", "message": limit.Error(), "limit_scope": limit.Scope, "retry_after_seconds": seconds, "reset_at": limit.ResetAt}
	} else {
		k := playgroundFailure(e)
		body["error"] = k
		status = contracts.ErrorMapping(k.Code).HTTP
		switch k.Code {
		case "request_too_large":
			status = 413
		case "playground_unavailable", "storage_error":
			status = 503
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
