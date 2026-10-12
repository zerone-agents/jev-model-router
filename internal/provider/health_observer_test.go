package provider

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"testing"
)

type observationStore struct {
	writes int
	last   health.Record
}

func (*observationStore) LoadHealthKey(context.Context) ([32]byte, error)     { return [32]byte{1}, nil }
func (*observationStore) LoadHealth(context.Context) ([]health.Record, error) { return nil, nil }
func (s *observationStore) SaveHealth(_ context.Context, r health.Record) error {
	s.writes++
	s.last = r
	return nil
}

type eventSource struct {
	events []routing.Event
	err    error
}

func (s *eventSource) Next(context.Context) (routing.Event, error) {
	if len(s.events) > 0 {
		e := s.events[0]
		s.events = s.events[1:]
		return e, nil
	}
	return routing.Event{}, s.err
}
func (*eventSource) Close() error { return nil }
func TestHealthStreamTerminalObservation(t *testing.T) {
	for _, mode := range []string{"success", "truncated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			store := &observationStore{}
			service := health.New(store, health.DefaultPolicy(), nil)
			g := &bifrostGenerator{health: service}
			cred := &preparedCredential{id: "target"}
			target := routing.Target{Prepared: cred}
			source := &eventSource{err: io.EOF}
			stop := "stop"
			if mode == "success" {
				source.events = []routing.Event{{Choices: []routing.Choice{{FinishReason: &stop}}}}
			}
			s := &observedChat{source: source, ctx: context.Background(), done: g.observe(target)}
			if mode != "cancelled" {
				for {
					_, e := s.Next(context.Background())
					if e != nil {
						break
					}
				}
			}
			s.Close()
			s.Close()
			switch mode {
			case "success":
				if store.writes != 1 || store.last.State != health.Healthy {
					t.Fatalf("success %+v", store)
				}
			case "truncated":
				if store.writes != 1 || len(store.last.FailureTimes) != 1 {
					t.Fatalf("truncated %+v", store)
				}
			case "cancelled":
				if store.writes != 0 {
					t.Fatal("cancellation counted")
				}
			}
		})
	}
}
