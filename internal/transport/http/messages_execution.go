package httptransport

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
)

func messagesSemanticEvent(e provider.MessageEvent) bool {
	var body map[string]json.RawMessage
	if json.Unmarshal(e.Data, &body) != nil {
		return true
	}
	var block map[string]json.RawMessage
	switch e.Type {
	case "content_block_start":
		if json.Unmarshal(body["content_block"], &block) != nil {
			return true
		}
		var kind string
		_ = json.Unmarshal(block["type"], &kind)
        if kind != "text" && kind != "thinking" {
			return true
		}
		var txt string
        _ = json.Unmarshal(block[kind], &txt)
		return txt != ""
	case "content_block_delta":
        if json.Unmarshal(body["delta"], &block)!=nil{return true}
        var kind string
        _ = json.Unmarshal(block["type"], &kind)
        field:=""
        switch kind {case "text_delta":field="text";case "thinking_delta":field="thinking";default:return true}
        var value string
        if json.Unmarshal(block[field],&value)!=nil{return true}
        return value!=""
	case "message_start", "message_delta", "message_stop", "content_block_stop", "ping":
		return false
	default:
		return true
	}
}

type replayMessages struct {
	events []provider.MessageEvent
	source provider.MessageStream
	ended  bool
}

func (s *replayMessages) Next(ctx context.Context) (provider.MessageEvent, error) {
	if len(s.events) > 0 {
		e := s.events[0]
		s.events = s.events[1:]
		return e, nil
	}
	if s.ended {
		return provider.MessageEvent{}, io.EOF
	}
	return s.source.Next(ctx)
}
func (s *replayMessages) Close() error { return s.source.Close() }
func prepareMessagesStream(ctx context.Context, source provider.MessageStream) (provider.MessageStream, error) {
	out := &replayMessages{source: source}
	var budget preambleBudget
	terminal := false
	for {
		e, err := source.Next(ctx)
		if err == io.EOF {
			if !terminal {
				return nil, routing.TemporaryUpstream(routing.Fail("upstream_error", "stream ended without completion"))
			}
			out.ended = true
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if messagesSemanticEvent(e) {
			out.events = append(out.events, e)
			return out, nil
		}
		if err = budget.add(len(e.Data) + len(e.Type) + 16); err != nil {
			return nil, err
		}
		out.events = append(out.events, e)
		terminal = e.Terminal
	}
}
