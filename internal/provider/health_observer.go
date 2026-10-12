package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"strings"
	"sync"
	"time"
)

func (g *bifrostGenerator) SetHealth(s *health.Service) { g.health = s }
func (g *bifrostGenerator) observe(t routing.Target) func(context.Context, error) {
	id := t.HealthIdentity()
	if g.health == nil || id == "" {
		return func(context.Context, error) {}
	}
	seq := g.health.Begin(id)
	var once sync.Once
	return func(ctx context.Context, e error) {
		once.Do(func() {
			if cause := context.Cause(ctx); cause != nil {
				e = cause
			}
			// Observations survive caller cancellation but cannot stall a response indefinitely.
			save, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = g.health.Observe(save, id, seq, routing.HealthOutcome(e))
		})
	}
}
func (g *bifrostGenerator) Complete(ctx context.Context, t routing.Target, r routing.Request) (routing.Completion, error) {
	done := g.observe(t)
	v, e := g.complete(ctx, t, r)
	done(ctx, e)
	return v, e
}
func (g *bifrostGenerator) Stream(ctx context.Context, t routing.Target, r routing.Request) (routing.EventStream, error) {
	done := g.observe(t)
	v, e := g.stream(ctx, t, r)
	if e != nil {
		done(ctx, e)
		return nil, e
	}
	if g.health == nil {
		return v, nil
	}
	return &observedChat{source: v, ctx: ctx, done: done}, nil
}
func (g *bifrostGenerator) CompleteMessages(ctx context.Context, t routing.Target, r *PreparedMessages) (json.RawMessage, error) {
	done := g.observe(t)
	v, e := g.completeMessages(ctx, t, r)
	done(ctx, e)
	return v, e
}
func (g *bifrostGenerator) StreamMessages(ctx context.Context, t routing.Target, r *PreparedMessages) (MessageStream, error) {
	done := g.observe(t)
	v, e := g.streamMessages(ctx, t, r)
	if e != nil {
		done(ctx, e)
		return nil, e
	}
	if g.health == nil {
		return v, nil
	}
	return &observedMessages{source: v, ctx: ctx, done: done}, nil
}

type observedChat struct {
	source   routing.EventStream
	ctx      context.Context
	done     func(context.Context, error)
	terminal bool
}

func (s *observedChat) Next(ctx context.Context) (routing.Event, error) {
	v, e := s.source.Next(ctx)
	for _, c := range v.Choices {
		if c.FinishReason != nil {
			s.terminal = true
		}
	}
	if e != nil {
		observed := e
		if errors.Is(e, io.EOF) {
			if s.terminal {
				observed = nil
			} else {
				observed = io.ErrUnexpectedEOF
			}
		}
		s.done(ctx, observed)
	}
	return v, e
}
func (s *observedChat) Close() error { s.done(s.ctx, context.Canceled); return s.source.Close() }

type observedMessages struct {
	source   MessageStream
	ctx      context.Context
	done     func(context.Context, error)
	terminal bool
}

func (s *observedMessages) Next(ctx context.Context) (MessageEvent, error) {
	v, e := s.source.Next(ctx)
	if v.Terminal {
		s.terminal = true
	}
	if e != nil {
		observed := e
		if errors.Is(e, io.EOF) {
			if s.terminal {
				observed = nil
			} else {
				observed = io.ErrUnexpectedEOF
			}
		}
		s.done(ctx, observed)
	}
	return v, e
}
func (s *observedMessages) Close() error { s.done(s.ctx, context.Canceled); return s.source.Close() }

func retryAfter(ctx context.Context) string {
	headers, _ := ctx.Value(schemas.BifrostContextKeyProviderResponseHeaders).(map[string]string)
	return retryAfterHeaders(headers)
}
func retryAfterHeaders(headers map[string]string) string {
	for k, v := range headers {
		if strings.EqualFold(k, "Retry-After") {
			return v
		}
	}
	return ""
}

// CloseWithCause preserves an idle deadline enforced by a buffered consumer.
func (s *observedChat) CloseWithCause(cause error) error {
	s.done(context.Background(), cause)
	return s.source.Close()
}
