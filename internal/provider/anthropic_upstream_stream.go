package provider

import (
	"context"
	"encoding/json"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"reflect"
	"strings"
	"sync"
)

type anthropicUpstreamStream struct {
	ctx       *schemas.BifrostContext
	ch        chan *schemas.BifrostStreamChunk
	state     *anthropicUpstreamValidation
	release   func()
	once      sync.Once
	done      bool
	text      strings.Builder
	reasoning strings.Builder
	calls     []routing.ToolCall
	id        string
}

func (s *anthropicUpstreamStream) Close() error {
	s.once.Do(func() { s.ctx.Cancel(); s.release() })
	return nil
}
func (s *anthropicUpstreamStream) Next(ctx context.Context) (routing.Event, error) {
	if s.done {
		return routing.Event{}, io.EOF
	}
	for {
		select {
		case <-ctx.Done():
			s.done = true
			s.Close()
			return routing.Event{}, anthropicUpstreamError(ctx, nil)
		case <-s.ctx.Done():
			s.done = true
			e := anthropicUpstreamError(s.ctx, nil)
			s.Close()
			return routing.Event{}, e
		case chunk, ok := <-s.ch:
			// A ready SDK channel may win select after cancellation. Classify
			// the context before consuming its reader error or terminal result.
			if ctx.Err() != nil || s.ctx.Err() != nil {
				s.done = true
				cause := ctx
				if cause.Err() == nil {
					cause = s.ctx
				}
				e := anthropicUpstreamError(cause, nil)
				s.Close()
				return routing.Event{}, e
			}
			if !ok {
				s.done = true
				out, e := s.finish()
				s.Close()
				return out, e
			}
			if chunk == nil {
				continue
			}
			if chunk.BifrostError != nil {
				s.done = true
				s.state.mu.Lock()
				e := s.state.failure
				s.state.mu.Unlock()
				if e == nil {
					e = anthropicUpstreamError(s.ctx, chunk.BifrostError)
				}
				s.Close()
				return routing.Event{}, e
			}
			if chunk.BifrostChatResponse == nil {
				s.done = true
				s.Close()
				return routing.Event{}, nativeError(502)
			}
			out, e := completion(chunk.BifrostChatResponse)
			if e != nil {
				s.done = true
				s.Close()
				return routing.Event{}, nativeError(502)
			}
			out.Usage = nil
			if out.ID != "" {
				s.id = out.ID
			}
			choices := out.Choices[:0]
			for _, c := range out.Choices {
				c.FinishReason = nil
				if c.Delta == nil {
					continue
				}
				d := c.Delta
				if len(d.Content) > 0 {
					var v string
					if json.Unmarshal(d.Content, &v) != nil {
						s.done = true
						s.Close()
						return routing.Event{}, nativeError(502)
					}
					s.text.WriteString(v)
				}
				if d.ReasoningContent != nil {
					s.reasoning.WriteString(*d.ReasoningContent)
				}
				for _, call := range d.ToolCalls {
					if call.Index == nil || *call.Index < 0 || *call.Index > len(s.calls) {
						s.done = true
						s.Close()
						return routing.Event{}, nativeError(502)
					}
					i := *call.Index
					if i == len(s.calls) {
						s.calls = append(s.calls, routing.ToolCall{})
					}
					existing := &s.calls[i]
					if call.ID != "" {
						if existing.ID != "" && existing.ID != call.ID {
							s.done = true
							s.Close()
							return routing.Event{}, nativeError(502)
						}
						existing.ID = call.ID
					}
					if call.Function.Name != "" {
						if existing.Function.Name != "" && existing.Function.Name != call.Function.Name {
							s.done = true
							s.Close()
							return routing.Event{}, nativeError(502)
						}
						existing.Function.Name = call.Function.Name
					}
					existing.Function.Arguments += call.Function.Arguments
				}
				if len(d.Content) > 0 || d.ReasoningContent != nil || len(d.ToolCalls) > 0 || d.Role != "" {
					choices = append(choices, c)
				}
			}
			out.Choices = choices
			if len(choices) > 0 {
				return out, nil
			}
		}
	}
}
func (s *anthropicUpstreamStream) finish() (routing.Event, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	state := s.state
	if state.failure != nil {
		return routing.Event{}, state.failure
	}
	if !state.terminalVerified {
		return routing.Event{}, nativeError(502)
	}
	var text, reason strings.Builder
	calls := []nativeBlock{}
	for _, b := range state.body.Content {
		switch b.Type {
		case "text":
			text.WriteString(*b.Text)
		case "thinking":
			reason.WriteString(*b.Thinking)
		case "tool_use":
			calls = append(calls, b)
		}
	}
	if s.text.String() != text.String() || s.reasoning.String() != reason.String() || len(calls) != len(s.calls) {
		return routing.Event{}, nativeError(502)
	}
	for i, b := range calls {
		c := s.calls[i]
		var args map[string]any
		if json.Unmarshal([]byte(c.Function.Arguments), &args) != nil || args == nil || c.ID != b.ID || c.Function.Name != b.Name || !reflect.DeepEqual(args, decoded(b.Input)) {
			return routing.Event{}, nativeError(502)
		}
	}
	finish, e := nativeFinish(state.body.Stop)
	if e != nil {
		return routing.Event{}, e
	}
	if (finish == "tool_calls") != (len(calls) > 0) {
		return routing.Event{}, nativeError(502)
	}
	out := routing.Event{ID: s.id, Choices: []routing.Choice{{Index: 0, Delta: &routing.Delta{}, FinishReason: &finish}}}
	if state.body.Usage != nil {
		out.Usage, e = state.body.Usage.normalized()
	}
	return out, e
}
