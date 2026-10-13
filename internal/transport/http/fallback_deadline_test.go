package httptransport

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type budgetWaitMessages struct{}

func (budgetWaitMessages) Next(ctx context.Context) (provider.MessageEvent, error) {
	<-ctx.Done()
	return provider.MessageEvent{}, ctx.Err()
}
func (budgetWaitMessages) Close() error { return nil }
func TestMessagesBudgetError(t *testing.T) {
	ctx, cancel := routing.WithRequestBudget(context.Background(), time.Now().Add(15*time.Millisecond))
	defer cancel()
	w := httptest.NewRecorder()
	e := writeMessagesStream(ctx, w, budgetWaitMessages{}, provider.MessageEvent{Type: "content_block_delta", Data: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)}, "req", time.Second)
	if routing.Diagnose(e).Code != "timeout" {
		t.Fatalf("wrong error %v", e)
	}
	if !strings.Contains(w.Body.String(), "event: error") || !strings.Contains(w.Body.String(), "timeout") {
		t.Fatalf("budget timeout has no protocol error: %s", w.Body.String())
	}
}

type deadlineWriter struct {
	header   http.Header
	mu       sync.Mutex
	deadline time.Time
	changed  chan struct{}
	started  chan struct{}
	once     sync.Once
}

func (w *deadlineWriter) Header() http.Header { return w.header }
func (w *deadlineWriter) WriteHeader(int)     {}
func (w *deadlineWriter) SetWriteDeadline(d time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = d
	if w.changed != nil {
		close(w.changed)
	}
	w.changed = make(chan struct{})
	return nil
}
func (w *deadlineWriter) Flush() {}
func (w *deadlineWriter) Write([]byte) (int, error) {
	if w.started != nil {
		w.once.Do(func() { close(w.started) })
	}
	for {
		w.mu.Lock()
		d, changed := w.deadline, w.changed
		w.mu.Unlock()
		timer := time.NewTimer(time.Until(d))
		select {
		case <-timer.C:
			return 0, errors.New("blocked write timeout")
		case <-changed:
			timer.Stop()
		}
	}
}
func TestPublicWriteHonorsBudget(t *testing.T) {
	for _, mode := range []string{"chat", "messages", "json", "terminal", "playground"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := routing.WithRequestBudget(context.Background(), time.Now().Add(20*time.Millisecond))
			defer cancel()
			w := &deadlineWriter{header: http.Header{}}
			start := time.Now()
			var err error
			switch mode {
			case "chat":
				err = writeStream(ctx, w, &fallbackSource{events: []routing.Event{{Choices: []routing.Choice{{Delta: &routing.Delta{Role: "assistant"}}}}}}, "a", "req", time.Second)
			case "messages":
				err = writeMessagesStream(ctx, w, budgetWaitMessages{}, provider.MessageEvent{Type: "message_start", Data: []byte(`{}`)}, "req", time.Second)
			case "playground":
				err = writePlaygroundStream(ctx, w, &fallbackSource{events: []routing.Event{{Choices: []routing.Choice{{Delta: &routing.Delta{Content: []byte(`"hello"`)}}}}}}, inference.RouteMetadata{RequestID: "req"})
			case "json":
				err = writeWithinRequest(ctx, w, time.Second, func() error { return WriteCompletion(w, routing.Completion{}, "a", "req") })
			case "terminal":
				<-ctx.Done()
				err = writeWithinRequest(ctx, w, time.Second, func() error { _, e := w.Write([]byte("error")); return e })
			}
			if err == nil {
				t.Fatal("expected downstream timeout")
			}
			if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
				t.Fatalf("%s write outlived request budget: %v", mode, elapsed)
			}
		})
	}
}
func TestResponseWriteInterruptedByClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &deadlineWriter{header: http.Header{}, started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- writeWithinRequest(ctx, w, time.Second, func() error { _, err := w.Write([]byte("body")); return err })
	}()
	<-w.started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancelled write")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("client cancellation did not interrupt write")
	}
}
