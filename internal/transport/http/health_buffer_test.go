package httptransport

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
	"time"
)

type healthBlockedStream struct{ cause chan error }

func (s *healthBlockedStream) Next(ctx context.Context) (routing.Event, error) {
	<-ctx.Done()
	s.cause <- context.Cause(ctx)
	return routing.Event{}, ctx.Err()
}
func (s *healthBlockedStream) Close() error { return nil }
func TestBufferedStreamPreservesIdleTimeoutCause(t *testing.T) {
	source := &healthBlockedStream{cause: make(chan error, 1)}
	stream := bufferStream(context.Background(), source, 1)
	defer stream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _ = stream.Next(ctx)
	select {
	case cause := <-source.cause:
		if !errors.Is(cause, context.DeadlineExceeded) {
			t.Fatalf("cause = %v", cause)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream reader did not receive timeout")
	}
}
