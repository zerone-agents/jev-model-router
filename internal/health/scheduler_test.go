package health

import (
	"context"
	"testing"
	"time"
)

func TestProbeMutualExclusion(t *testing.T) {
	p := NewProbeLimiter(2)
	release, e := p.Acquire("a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Acquire("a"); e == nil {
		t.Fatal("duplicate probe admitted")
	}
	r, e := p.Acquire("b")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Acquire("c"); e == nil {
		t.Fatal("unbounded concurrency")
	}
	release()
	r()
	release, e = p.Acquire("a")
	if e != nil {
		t.Fatal(e)
	}
	release()
}
func TestProbeShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { RunScheduler(ctx, time.Millisecond, func(context.Context) {}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}
