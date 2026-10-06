package httptransport

import (
	"context"
	"errors"
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
