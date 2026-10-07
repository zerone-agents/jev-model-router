package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type MessageEvent struct {
	Type     string
	Data     json.RawMessage
	Terminal bool
}
type MessageStream interface {
	Next(context.Context) (MessageEvent, error)
	Close() error
}
type toolFragments struct{ id, name, arguments string }

// Bifrost's Responses converter accumulates output, not just a bounded queue.
// Bound the serialized decoded frames and their number before it sees them.
const maxMessagesStreamBytes int64 = 16 << 20
const maxMessagesStreamFrames = 65536

type messagesStream struct {
	ctx        *schemas.BifrostContext
	ch         chan *schemas.BifrostStreamChunk
	state      *schemas.ChatToResponsesStreamState
	release    func()
	once       sync.Once
	queue      []MessageEvent
	tail       []*anthropic.AnthropicStreamEvent
	usage      *routing.Usage
	finish     *string
	stopString *string
	model      string
	stops      bool
	ended      bool
	tools      map[int]*toolFragments
	byteLimit  int64
	bytes      int64
	frames     int
}

func (g *bifrostGenerator) StreamMessages(ctx context.Context, t routing.Target, p *PreparedMessages) (MessageStream, error) {
	if err := p.Check(t); err != nil {
		return nil, err
	}
	client, release, err := g.acquire(t)
	if err != nil {
		return nil, err
	}
	bc := schemas.NewBifrostContext(ctx, time.Time{})
	bc.SetValue(schemas.BifrostContextKeyAllowPerRequestRawOverride, true)
	bc.SetValue(schemas.BifrostContextKeySendBackRawResponse, true)
	r, err := encode(bc, t, p.Projection())
	if err != nil {
		bc.Cancel()
		release()
		return nil, err
	}
	ch, failure := client.ChatCompletionStreamRequest(bc, r)
	if failure != nil {
		bc.Cancel()
		release()
		return nil, providerError(ctx, failure)
	}
	return &messagesStream{ctx: bc, ch: ch, state: schemas.AcquireChatToResponsesStreamState(), release: release, model: t.Model.ID, stops: p.stops, tools: map[int]*toolFragments{}, byteLimit: maxMessagesStreamBytes}, nil
}
func (s *messagesStream) Close() error {
	s.once.Do(func() { s.ctx.Cancel(); schemas.ReleaseChatToResponsesStreamState(s.state); s.release() })
	return nil
}
func (s *messagesStream) Next(ctx context.Context) (MessageEvent, error) {
	for {
		if len(s.queue) > 0 {
			e := s.queue[0]
			s.queue = s.queue[1:]
			return e, nil
		}
		if s.ended {
			return MessageEvent{}, io.EOF
		}
		var chunk *schemas.BifrostStreamChunk
		var ok bool
		select {
		case <-ctx.Done():
			return MessageEvent{}, safeError(ctx)
		case <-s.ctx.Done():
			return MessageEvent{}, safeError(s.ctx)
		case chunk, ok = <-s.ch:
		}
		if !ok {
			s.ended = true
			if err := validMessagesFinish(s.finish, s.stops && (s.stopString == nil || *s.stopString == "")); err != nil {
				return MessageEvent{}, err
			}
			ids := map[string]bool{}
			for _, tool := range s.tools {
				if tool.id == "" || tool.name == "" || ids[tool.id] {
					return MessageEvent{}, routing.Fail("upstream_error", "invalid upstream tool call")
				}
				ids[tool.id] = true
				if err := validateToolArguments(tool.arguments); err != nil {
					return MessageEvent{}, err
				}
			}
			if len(s.tail) == 0 {
				return MessageEvent{}, routing.Fail("upstream_error", "stream ended without Messages completion")
			}
			for _, e := range s.tail {
				if string(e.Type) == "message_delta" && s.stopString != nil && *s.stopString != "" {
					e.Delta.StopReason = schemas.Ptr(anthropic.AnthropicStopReason("stop_sequence"))
					e.Delta.StopSequence = s.stopString
				}
				encoded, err := encodeMessageEvent(e, s.usage)
				if err != nil {
					return MessageEvent{}, err
				}
				s.queue = append(s.queue, encoded)
			}
			continue
		}
		if chunk == nil {
			return MessageEvent{}, routing.Fail("upstream_error", "invalid stream event")
		}
		if chunk.BifrostError != nil {
			return MessageEvent{}, providerError(s.ctx, chunk.BifrostError)
		}
		r := chunk.BifrostChatResponse
		if r == nil {
			return MessageEvent{}, routing.Fail("upstream_error", "invalid stream event")
		}
		if captured, ok := r.ExtraFields.RawResponse.(string); ok && int64(len(captured)) > s.byteLimit {
			return MessageEvent{}, routing.Fail("upstream_error", "Messages stream exceeds response byte limit")
		}
		usage, err := reportedMessagesUsage(r.ExtraFields.RawResponse)
		if err != nil {
			return MessageEvent{}, err
		}
		if usage != nil {
			s.usage = usage
		}
		matched, err := reportedMessagesStop(r.ExtraFields.RawResponse)
		if err != nil {
			return MessageEvent{}, err
		}
		if matched != nil {
			s.stopString = matched
		}
		// Raw captures are consumed only inside this adapter. Include all public
		// frame metadata (IDs, tool names, etc.) in the accumulator budget.
		r.ExtraFields = schemas.BifrostResponseExtraFields{}
		frame, err := json.Marshal(r)
		if err != nil {
			return MessageEvent{}, routing.Fail("upstream_error", "invalid stream event")
		}
		s.frames++
		if s.frames > maxMessagesStreamFrames || int64(len(frame)) > s.byteLimit-s.bytes {
			return MessageEvent{}, routing.Fail("upstream_error", "Messages stream exceeds response byte or event limit")
		}
		s.bytes += int64(len(frame))
		if len(r.Choices) > 1 {
			return MessageEvent{}, routing.Fail("upstream_error", "multiple upstream choices are not supported")
		}
		for _, choice := range r.Choices {
			if s.finish != nil {
				// Bifrost sends a trailing aggregate with the same finish reason.
				// It carries usage, not a second generation turn.
				meaningful := false
				if choice.ChatStreamResponseChoice != nil && choice.Delta != nil {
					d := choice.Delta
					meaningful = len(d.ToolCalls) > 0 || (d.Content != nil && *d.Content != "") || (d.Reasoning != nil && *d.Reasoning != "") || d.Refusal != nil
				}
				if meaningful || (choice.FinishReason != nil && *choice.FinishReason != *s.finish) {
					return MessageEvent{}, routing.Fail("upstream_error", "upstream output after stop reason")
				}
				continue
			}
			if choice.ChatStreamResponseChoice != nil && choice.Delta != nil {
				if choice.Delta.Refusal != nil && *choice.Delta.Refusal != "" {
					return MessageEvent{}, routing.Fail("upstream_error", "upstream refusal cannot be represented by the Messages converter")
				}
				for _, call := range choice.Delta.ToolCalls {
					if call.Index >= 128 {
						return MessageEvent{}, routing.Fail("upstream_error", "invalid upstream tool index")
					}
					tool := s.tools[int(call.Index)]
					if tool == nil {
						tool = &toolFragments{}
						s.tools[int(call.Index)] = tool
					}
					if call.ID != nil {
						if tool.id != "" && tool.id != *call.ID {
							return MessageEvent{}, routing.Fail("upstream_error", "upstream tool id changed")
						}
						tool.id = *call.ID
					}
					if call.Function.Name != nil {
						tool.name += *call.Function.Name
					}
					tool.arguments += call.Function.Arguments
					if len(tool.arguments) > 1048576 {
						return MessageEvent{}, routing.Fail("upstream_error", "upstream tool input too large")
					}
				}
			}
			if choice.FinishReason != nil {
				if err := validMessagesFinish(choice.FinishReason, false); err != nil {
					return MessageEvent{}, err
				}
				s.finish = choice.FinishReason
				if choice.ChatNonStreamResponseChoice != nil && choice.StopString != nil {
					s.stopString = choice.StopString
				}
			}
		}
		if len(s.tail) > 0 {
			continue
		}
		r.ExtraFields = schemas.BifrostResponseExtraFields{}
		if s.usage == nil {
			r.Usage = nil
		}
		r.Model = s.model
		if !s.state.HasEmittedCreated && len(r.Choices) > 0 && r.Choices[0].ChatStreamResponseChoice != nil && r.Choices[0].Delta != nil {
			r.Choices[0].Delta.Role = schemas.Ptr("assistant")
		}
		for _, converted := range messagesResponsesFrames(r, s.state) {
			if converted.Response != nil {
				converted.Response.Model = s.model
			}
			for _, e := range anthropic.ToAnthropicResponsesStreamResponse(s.ctx, converted) {
				if string(e.Type) == "message_delta" || string(e.Type) == "message_stop" {
					s.tail = append(s.tail, e)
					continue
				}
				encoded, err := encodeMessageEvent(e, s.usage)
				if err != nil {
					return MessageEvent{}, err
				}
				s.queue = append(s.queue, encoded)
			}
		}
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// The pinned converter handles one tool delta per invocation. Split only the
// tool array; emit content once and finish only after every tool fragment.
func messagesResponsesFrames(r *schemas.BifrostChatResponse, state *schemas.ChatToResponsesStreamState) []*schemas.BifrostResponsesStreamResponse {
	if len(r.Choices) != 1 || r.Choices[0].ChatStreamResponseChoice == nil || r.Choices[0].Delta == nil || len(r.Choices[0].Delta.ToolCalls) < 2 {
		return r.ToBifrostResponsesStreamResponse(state)
	}
	original := r.Choices[0]
	var result []*schemas.BifrostResponsesStreamResponse
	for i := range original.Delta.ToolCalls {
		frame := *r
		choice := original
		streamChoice := *original.ChatStreamResponseChoice
		delta := *original.Delta
		delta.ToolCalls = original.Delta.ToolCalls[i : i+1]
		if i > 0 {
			delta.Content = nil
			delta.Reasoning = nil
			delta.Role = nil
			delta.Refusal = nil
		}
		if i < len(original.Delta.ToolCalls)-1 {
			choice.FinishReason = nil
		}
		streamChoice.Delta = &delta
		choice.ChatStreamResponseChoice = &streamChoice
		frame.Choices = append(r.Choices[:0:0], choice)
		result = append(result, frame.ToBifrostResponsesStreamResponse(state)...)
	}
	return result
}
