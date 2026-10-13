package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// decodeChatRequest is the common Chat Completions wire decoder. Entrypoints
// enforce their own authentication, body size and allowed-feature policies.
func decodeChatRequest(body io.Reader) (routing.Request, error) {
	var req routing.Request
	d := json.NewDecoder(body)
	if err := d.Decode(&req); err != nil {
		var fieldError *routing.Error
		if errors.As(err, &fieldError) {
			return req, fieldError
		}
		return req, routing.Fail("invalid_request", "invalid chat body")
	}
	if d.Decode(new(any)) != io.EOF {
		return req, routing.Fail("invalid_request", "invalid chat body")
	}
	return req, nil
}

type chatOutput struct {
	planned  func(routing.Plan) error
	complete func(routing.Completion, routing.Plan) error
	stream   func(context.Context, routing.EventStream, routing.Plan) error
}

// executeChat owns planning, provider dispatch and the first-event gate for
// both the public API and Playground. Only policy and response encoding vary.
// A zero buffer reads synchronously, retaining Playground's quota lease until
// the provider reader exits; the public API retains its configured buffer.
func executeChat(ctx context.Context, execution *inference.Request, executor *routing.Executor, req routing.Request, firstTimeout time.Duration, bufferSize int, output chatOutput) error {
	return execution.Execute(req, nil, nil, routing.RequestSummary(req), func(attemptCtx context.Context, plan routing.Plan, _ int) inference.AttemptResult {
		if output.planned != nil {
			if e := output.planned(plan); e != nil {
				return inference.AttemptResult{Err: e, Committed: true}
			}
		}
		if !req.Stream {
			callCtx, cancel := routing.WithStageTimeout(attemptCtx, firstTimeout)
			defer cancel()
			result, e := executor.Complete(callCtx, plan, req)
			e = contextError(callCtx, e)
			if e != nil {
				return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
			}
			return inference.AttemptResult{Err: output.complete(result, plan), Committed: true}
		}
		streamCtx, stop := context.WithCancelCause(attemptCtx)
		defer stop(context.Canceled)
		firstCtx, cancel := routing.WithStageTimeout(streamCtx, firstTimeout)
		defer cancel()
		stopOpening := context.AfterFunc(firstCtx, func() { stop(context.Cause(firstCtx)) })
		defer stopOpening()
		source, e := executor.Stream(streamCtx, plan, req)
		if e != nil {
			e = contextError(firstCtx, e)
			return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
		}
		defer source.Close()

		replay, e := prepareChatStream(firstCtx, source)
		if e != nil {
			e = contextError(firstCtx, e)
			return inference.AttemptResult{Err: e, RetryableGenerationFailure: routing.HealthOutcome(e).Kind != ""}
		}
		if !stopOpening() {
			return inference.AttemptResult{Err: contextError(firstCtx, firstCtx.Err()), RetryableGenerationFailure: true}
		}
		return inference.AttemptResult{Err: output.stream(streamCtx, diagnosticChat{replay, execution}, plan), Committed: true}
	})
}
