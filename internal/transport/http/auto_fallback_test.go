package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"testing"
	"time"
)

type fallbackDecider struct{}

func (fallbackDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	return routing.Decision{ModelID: in.Candidates[0].ID}, nil
}

type fallbackSource struct {
	events []routing.Event
	err    error
}

func (s *fallbackSource) Next(context.Context) (routing.Event, error) {
	if len(s.events) > 0 {
		e := s.events[0]
		s.events = s.events[1:]
		return e, nil
	}
	return routing.Event{}, s.err
}
func (s *fallbackSource) Close() error { return nil }

type fallbackGenerator struct {
	calls    int
	semantic bool
}

func (g *fallbackGenerator) Complete(context.Context, routing.Target, routing.Request) (routing.Completion, error) {
	return routing.Completion{}, nil
}
func (g *fallbackGenerator) Stream(context.Context, routing.Target, routing.Request) (routing.EventStream, error) {
	g.calls++
	d := routing.Delta{Role: "assistant"}
	if g.semantic {
		d.Content = json.RawMessage(`"first"`)
	}
	if g.calls == 1 {
		return &fallbackSource{events: []routing.Event{{Choices: []routing.Choice{{Delta: &d}}}}, err: &routing.UpstreamError{Status: 503}}, nil
	}
	d.Content = json.RawMessage(`"second"`)
	stop := "stop"
	return &fallbackSource{events: []routing.Event{{Choices: []routing.Choice{{Delta: &d, FinishReason: &stop}}}}, err: io.EOF}, nil
}
func TestFallbackSemanticBoundary(t *testing.T) {
	for _, semantic := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata", true: "content"}[semantic], func(t *testing.T) {
			s := snapshot()
			s.Models[0].Capabilities.ContextLimit = 8000
			s.Decision.Model = "jev"
			other := s.Models[0]
			other.ID = "backup"
			s.Models = append(s.Models, other)
			svc := &inference.Service{Store: testStore{s}, Planner: &routing.Planner{Decider: fallbackDecider{}}, DecisionTimeout: time.Second, Fallback: inference.DefaultFallbackConfig()}
			execution := svc.Begin(context.Background(), "test")
			g := &fallbackGenerator{semantic: semantic}
			req := routing.Request{Model: "auto", Stream: true, Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
			var contents string
			err := executeChat(context.Background(), execution, &routing.Executor{Generator: g}, req, time.Second, 8, chatOutput{stream: func(ctx context.Context, s routing.EventStream, _ routing.Plan) error {
				for {
					e, err := s.Next(ctx)
					if err == io.EOF {
						return nil
					}
					if err != nil {
						return err
					}
					for _, c := range e.Choices {
						if c.Delta != nil {
							contents += string(c.Delta.Content)
						}
					}
				}
			}})
			if semantic {
				if g.calls != 1 || err == nil {
					t.Fatalf("calls=%d err=%v", g.calls, err)
				}
			} else if g.calls != 2 || err != nil || contents != `"second"` {
				t.Fatalf("calls=%d err=%v contents=%s", g.calls, err, contents)
			}
		})
	}
}
func TestFallbackPartialWriterNeverRetries(t *testing.T) {
	s := snapshot()
	svc := &inference.Service{Store: testStore{s}, Planner: &routing.Planner{}, DecisionTimeout: time.Second, Fallback: inference.DefaultFallbackConfig()}
	r := svc.Begin(context.Background(), "write")
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	calls := 0
	err := executeChat(context.Background(), r, &routing.Executor{Generator: &fallbackGenerator{}}, req, time.Second, 0, chatOutput{complete: func(routing.Completion, routing.Plan) error { calls++; return errors.New("partial downstream write") }})
	if err == nil || calls != 1 || len(r.Attempts()) != 1 {
		t.Fatalf("%v %d", err, calls)
	}
}
