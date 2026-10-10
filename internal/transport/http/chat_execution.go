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
	complete func(routing.Completion, routing.Plan) error
	stream   func(context.Context, routing.EventStream, routing.Plan) error
}

// executeChat owns planning, provider dispatch and the first-event gate for
// both the public API and Playground. Only policy and response encoding vary.
// A zero buffer reads synchronously, retaining Playground's quota lease until
// the provider reader exits; the public API retains its configured buffer.
func executeChat(ctx context.Context, execution *inference.Request, executor *routing.Executor, req routing.Request, firstTimeout time.Duration, bufferSize int, output chatOutput) error {
	plan, err := execution.Plan(req, nil)
	if err != nil {
		return contextError(ctx, err)
	}
	if !req.Stream {
		callCtx, cancel := context.WithTimeout(ctx, firstTimeout)
		defer cancel()
		result, err := executor.Complete(callCtx, plan, req)
		if err = contextError(callCtx, err); err != nil {
			return err
		}
		return output.complete(result, plan)
	}
	streamCtx, stop := context.WithCancelCause(ctx)
	defer stop(context.Canceled)
	timer := time.AfterFunc(firstTimeout, func() { stop(context.DeadlineExceeded) })
	defer timer.Stop()
	source, err := executor.Stream(streamCtx, plan, req)
	if err != nil {
		return contextError(streamCtx, err)
	}
	if bufferSize > 0 {
		source = bufferStream(streamCtx, source, bufferSize)
	}
	defer source.Close()
	for {
		first, err := source.Next(streamCtx)
		if err != nil {
			return contextError(streamCtx, err)
		}
		if !validEvent(first) {
			continue
		}
		if !timer.Stop() {
			return routing.Fail("timeout", "first event timed out")
		}
		return output.stream(streamCtx, &prependStream{first: &first, rest: source}, plan)
	}
}
