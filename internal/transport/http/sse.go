package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"sync"
	"time"
)

type streamItem struct {
	event routing.Event
	err   error
}
type bufferedStream struct {
	ch     chan streamItem
	cancel context.CancelFunc
	source routing.EventStream
	once   sync.Once
}

func bufferStream(ctx context.Context, s routing.EventStream, size int) *bufferedStream {
	ctx, cancel := context.WithCancel(ctx)
	b := &bufferedStream{ch: make(chan streamItem, size), cancel: cancel, source: s}
	go func() {
		defer close(b.ch)
		for {
			if ctx.Err() != nil {
				return
			}
			event, e := s.Next(ctx)
			select {
			case b.ch <- streamItem{event, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	return b
}
func (b *bufferedStream) Next(ctx context.Context) (routing.Event, error) {
	select {
	case <-ctx.Done():
		return routing.Event{}, contextError(ctx, ctx.Err())
	case v, ok := <-b.ch:
		if !ok {
			return routing.Event{}, io.EOF
		}
		return v.event, v.err
	}
}
func (b *bufferedStream) Close() error {
	b.once.Do(func() { b.cancel(); b.source.Close() })
	return nil
}

type prependStream struct {
	first *routing.Event
	rest  routing.EventStream
}

func (p *prependStream) Next(ctx context.Context) (routing.Event, error) {
	if p.first != nil {
		v := *p.first
		p.first = nil
		return v, nil
	}
	return p.rest.Next(ctx)
}
func (p *prependStream) Close() error { return p.rest.Close() }
func validEvent(e routing.Event) bool {
	if e.Usage != nil {
		return true
	}
	for _, c := range e.Choices {
		if c.FinishReason != nil {
			return true
		}
		if c.Delta != nil {
			d := c.Delta
			if (d.ReasoningContent != nil && *d.ReasoningContent != "") || d.Role != "" || len(d.ToolCalls) > 0 || d.Refusal != nil || (len(d.Content) > 0 && string(d.Content) != `""` && string(d.Content) != "null") {
				return true
			}
		}
	}
	return false
}
func WriteStream(ctx context.Context, w http.ResponseWriter, stream routing.EventStream, modelID, requestID string, idle time.Duration) error {
	defer stream.Close()
	return writeStream(ctx, w, stream, modelID, requestID, idle)
}
func writeStream(ctx context.Context, w http.ResponseWriter, s routing.EventStream, model, id string, idle time.Duration) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Request-ID", id)
	controller := http.NewResponseController(w)
	finished := false
	deadline := time.Now().Add(idle)
	send := func(v any) error {
		controller.SetWriteDeadline(time.Now().Add(idle))
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintf(w, "data: %s\n\n", b); e != nil {
			return e
		}
		return controller.Flush()
	}
	for {
		nextCtx, cancel := context.WithDeadline(ctx, deadline)
		event, e := s.Next(nextCtx)
		e = contextError(nextCtx, e)
		cancel()
		if e == io.EOF {
			if !finished {
				e = routing.Fail("upstream_error", "stream ended without completion")
			} else {
				controller.SetWriteDeadline(time.Now().Add(idle))
				_, e = fmt.Fprint(w, "data: [DONE]\n\n")
				if e == nil {
					e = controller.Flush()
				}
				return e
			}
		}
		if e != nil {
			send(errorBody(e))
			return e
		}
		if !validEvent(event) {
			continue
		}
		for _, c := range event.Choices {
			if c.FinishReason != nil {
				finished = true
			}
		}
		if e = send(completionBody(event, model, "chat.completion.chunk")); e != nil {
			return e
		}
		deadline = time.Now().Add(idle)
	}
}
