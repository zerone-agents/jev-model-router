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
	r *inference.Request
}

func (s diagnosticChat) Next(ctx context.Context) (routing.Event, error) {
	e, err := s.EventStream.Next(ctx)
	if err != nil && err != io.EOF {
		err = s.r.StreamError(contextError(ctx, err))
	}
	return e, err
}

type diagnosticMessages struct {
	provider.MessageStream
	r *inference.Request
}

func (s diagnosticMessages) Next(ctx context.Context) (provider.MessageEvent, error) {
	e, err := s.MessageStream.Next(ctx)
	if err != nil && err != io.EOF {
		err = s.r.StreamError(contextError(ctx, err))
	}
	return e, err
}
