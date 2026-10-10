package httptransport

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// Bifrost enforces the inherited deadline with its own timer. It can return
// before the parent timer publishes Err; model that ordering deterministically.
type deadlinePending struct{ context.Context }

func (deadlinePending) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func TestContextErrorDeadlinePending(t *testing.T) {
	for _, code := range []string{"upstream_error", "cancelled"} {
		err := contextError(deadlinePending{context.Background()}, routing.Fail(code, "adapter failed"))
		var known *routing.Error
		if !errors.As(err, &known) || known.Code != "timeout" {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(deadlinePending{context.Background()})
	cancel()
	err := contextError(ctx, context.Canceled)
	var known *routing.Error
	if !errors.As(err, &known) || known.Code != "cancelled" {
		t.Fatal("explicit cancellation misclassified", err)
	}
}

// A timer may publish DeadlineExceeded between the error/cause checks and the
// cancellation branch. A single changing context must not become HTTP 499.
type deadlinePublishing struct {
	context.Context
	reads int
}

func (*deadlinePublishing) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func (c *deadlinePublishing) Err() error {
	c.reads++
	if c.reads >= 3 {
		return context.DeadlineExceeded
	}
	return nil
}
func TestMessagesDeadlinePublishingIsTimeout(t *testing.T) {
	ctx := &deadlinePublishing{Context: context.Background()}
	w := httptest.NewRecorder()
	writeMessagesError(w, contextError(ctx, routing.Fail("cancelled", "adapter timer fired")), "test-request")
	if w.Code != 504 || !strings.Contains(w.Body.String(), `"type":"timeout_error"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
