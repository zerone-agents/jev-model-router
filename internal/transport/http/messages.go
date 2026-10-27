package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type messagesGenerator interface {
	CompleteMessages(context.Context, routing.Target, *provider.PreparedMessages) (json.RawMessage, error)
	StreamMessages(context.Context, routing.Target, *provider.PreparedMessages) (provider.MessageStream, error)
}

func messagesPrincipal(r *http.Request, auth func(string) (management.Principal, error)) (management.Principal, error) {
	bad := func() (management.Principal, error) {
		return management.Principal{}, routing.Fail("unauthorized", "invalid inference credentials")
	}
	if len(r.Header.Values("x-api-key")) > 1 || len(r.Header.Values("Authorization")) > 1 {
		return bad()
	}
	key, bearer := r.Header.Get("x-api-key"), r.Header.Get("Authorization")
	if key != "" && (strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n\x00")) {
		return bad()
	}
	if key == "" {
		return auth(bearer)
	}
	p, err := auth("Bearer " + key)
	if err != nil {
		return bad()
	}
	if bearer != "" {
		q, err := auth(bearer)
		if err != nil || p.ID != q.ID || p.Role != q.Role {
			return bad()
		}
	}
	return p, nil
}
func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	id := w.Header().Get("X-Request-ID")
	execution := s.inference.Begin(r.Context(), id)
	var finalErr error
	defer func() { execution.Finish(finalErr) }()
	fail := func(err error) { finalErr = err; writeMessagesError(w, err, id) }
	if r.Header.Get("anthropic-version") != "2023-06-01" || len(r.Header.Values("anthropic-version")) != 1 {
		fail(routing.Fail("invalid_request", "anthropic-version must be 2023-06-01"))
		return
	}
	if r.Header.Get("anthropic-beta") != "" {
		fail(routing.Fail("unsupported_request", "anthropic-beta features are not supported"))
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(routing.Fail("invalid_request", "Content-Type must be application/json"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.limits.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			fail(routing.Fail("request_too_large", "body too large"))
		} else {
			fail(routing.Fail("invalid_request", "cannot read Messages body"))
		}
		return
	}
	prepared, err := provider.PrepareMessages(body)
	if err != nil {
		fail(err)
		return
	}

	gen, ok := s.executor.Generator.(messagesGenerator)
	if !ok {
		fail(routing.Fail("config_missing", "Messages generator unavailable"))
		return
	}
	started := false
	finalErr = execution.Execute(prepared.Projection(), prepared.Check, prepared.Estimate, prepared.Summary(), func(attemptCtx context.Context, plan routing.Plan, _ int) inference.AttemptResult {
		effective, err := prepared.WithEffectiveEffort(plan.EffectiveEffort)
		if err == nil {
			err = effective.Check(plan.Target)
		}
		if err != nil {
			return inference.AttemptResult{Err: err}
		}

		if !prepared.Projection().Stream {
			ctx, cancel := routing.WithStageTimeout(attemptCtx, s.limits.FirstEventTimeout)
			defer cancel()
			result, e := gen.CompleteMessages(ctx, plan.Target, effective)
			e = contextError(ctx, e)
			if e != nil {
				return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
			}
			started = true
			e = writeWithinRequest(attemptCtx, w, s.limits.IdleTimeout, func() error {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write(append(result, '\n'))
				return err
			})
			return inference.AttemptResult{Err: e, Committed: true}
		}
		ctx, stop := context.WithCancelCause(attemptCtx)
		defer stop(context.Canceled)
		firstCtx, cancel := routing.WithStageTimeout(ctx, s.limits.FirstEventTimeout)
		defer cancel()
		stopOpening := context.AfterFunc(firstCtx, func() { stop(context.Cause(firstCtx)) })
		defer stopOpening()
		stream, e := gen.StreamMessages(ctx, plan.Target, effective)
		if e != nil {
			e = contextError(firstCtx, e)
			return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
		}
		defer stream.Close()
		replay, e := prepareMessagesStream(firstCtx, stream)
		if e != nil {
			e = contextError(firstCtx, e)
			return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
		}
		if !stopOpening() {
			return inference.AttemptResult{Err: contextError(firstCtx, firstCtx.Err()), RetryableGenerationFailure: true}
		}
		first, e := replay.Next(ctx)
		if e != nil {
			return inference.AttemptResult{Err: e}
		}
		started = true
		return inference.AttemptResult{Err: writeMessagesStream(ctx, w, &diagnosticMessages{MessageStream: replay, r: execution, terminal: first.Terminal}, first, id, s.limits.IdleTimeout), Committed: true}
	})
	if finalErr != nil && !started {
		_ = writeWithinRequest(execution.Context(), w, s.limits.IdleTimeout, func() error { writeMessagesError(w, finalErr, id); return nil })
	}
}

func writeMessagesStream(ctx context.Context, w http.ResponseWriter, stream provider.MessageStream, first provider.MessageEvent, id string, idle time.Duration) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	controller := http.NewResponseController(w)
	send := func(e provider.MessageEvent) error {
		return writeWithinRequest(ctx, w, idle, func() error {
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, e.Data); err != nil {
				return err
			}
			return controller.Flush()
		})
	}
	if err := send(first); err != nil {
		return err
	}
	terminal := first.Terminal
	for {
		nextCtx, cancel := routing.WithStageTimeout(ctx, idle)
		e, err := stream.Next(nextCtx)
		err = contextError(nextCtx, err)
		cancel()
		if err == io.EOF && terminal {
			return nil
		}
		if err == io.EOF {
			err = routing.Fail("upstream_error", "stream ended without completion")
		}
		if err != nil {
			if ctx.Err() == nil || errors.Is(context.Cause(ctx), routing.ErrRequestBudgetExhausted) || errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				_, body := messagesError(err, id)
				b, _ := json.Marshal(body)
				_ = send(provider.MessageEvent{Type: "error", Data: b})
			}
			return err
		}
		if terminal {
			return routing.Fail("upstream_error", "event after Messages completion")
		}
		if err = send(e); err != nil {
			return err
		}
		terminal = e.Terminal
	}
}
