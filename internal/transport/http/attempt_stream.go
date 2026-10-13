package httptransport

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
)

type diagnosticChat struct {
	routing.EventStream
	r        *inference.Request
	terminal bool
}

func (s *diagnosticChat) Next(ctx context.Context) (routing.Event, error) {
	e, err := s.EventStream.Next(ctx)
	for _, c := range e.Choices {
		if c.FinishReason != nil {
			s.terminal = true
		}
	}
	if err == io.EOF && !s.terminal {
		err = routing.TemporaryUpstream(routing.Fail("upstream_error", "stream ended without completion"))
	}
	if err != nil && err != io.EOF {
		err = s.r.StreamError(contextError(ctx, err))
	}
	return e, err
}

type diagnosticMessages struct {
	provider.MessageStream
	r        *inference.Request
	terminal bool
}

func (s *diagnosticMessages) Next(ctx context.Context) (provider.MessageEvent, error) {
	e, err := s.MessageStream.Next(ctx)
	if e.Terminal {
		s.terminal = true
	}
	if err == io.EOF && !s.terminal {
		err = routing.TemporaryUpstream(routing.Fail("upstream_error", "stream ended without completion"))
	}
	if err != nil && err != io.EOF {
		err = s.r.StreamError(contextError(ctx, err))
	}
	return e, err
}
