package httptransport

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
)

type preambleBudget struct{ bytes, frames int }

func (b *preambleBudget) add(n int) error {
	b.bytes += n
	b.frames++
	if b.bytes > 64<<10 || b.frames > 128 {
		return routing.Fail("resource_exhausted", "stream preamble limit exceeded")
	}
	return nil
}
func chatSemanticEvent(e routing.Event) bool {
	for _, c := range e.Choices {
		d := c.Delta
		if d == nil {
			continue
		}
		if len(d.ToolCalls) > 0 || (d.ReasoningContent != nil && *d.ReasoningContent != "") || (d.Refusal != nil && *d.Refusal != "") {
			return true
		}
		if len(d.Content) > 0 && string(d.Content) != "null" && string(d.Content) != `""` {
			return true
		}
	}
	return false
}

type replayChat struct {
	events []routing.Event
	source routing.EventStream
	ended  bool
}

func (s *replayChat) Next(ctx context.Context) (routing.Event, error) {
	if len(s.events) > 0 {
		e := s.events[0]
		s.events = s.events[1:]
		return e, nil
	}
	if s.ended {
		return routing.Event{}, io.EOF
	}
	return s.source.Next(ctx)
}
func (s *replayChat) Close() error { return s.source.Close() }
func prepareChatStream(ctx context.Context, source routing.EventStream) (routing.EventStream, error) {
	out := &replayChat{source: source}
	var budget preambleBudget
	terminal := false
	for {
		event, e := source.Next(ctx)
		if e == io.EOF {
			if !terminal {
				return nil, routing.TemporaryUpstream(routing.Fail("upstream_error", "stream ended without completion"))
			}
			out.ended = true
			return out, nil
		}
		if e != nil {
			return nil, e
		}
		if chatSemanticEvent(event) {
			out.events = append(out.events, event)
			return out, nil
		}
		b, _ := json.Marshal(event)
		if e = budget.add(len(b)); e != nil {
			return nil, e
		}
		out.events = append(out.events, event)
		for _, c := range event.Choices {
			if c.FinishReason != nil {
				terminal = true
			}
		}
	}
}
