package httptransport

import (
	"context"
	"net/http"
	"time"
)

// writeWithinRequest applies both the idle limit and the shared request budget.
// Terminal errors are best effort: an expired budget never gets a fresh idle
// interval just to report its expiry. Cancellation also interrupts blocked writes.
func writeWithinRequest(ctx context.Context, w http.ResponseWriter, idle time.Duration, write func() error) error {
	controller := http.NewResponseController(w)
	var deadline time.Time
	if idle > 0 {
		deadline = time.Now().Add(idle)
	}
	if d, ok := ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
		deadline = d
	}
	if ctx.Err() != nil {
		deadline = time.Now()
	}
	_ = controller.SetWriteDeadline(deadline)
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(done)
	})
	defer func() {
		if !stop() {
			<-done
		}
	}()
	return write()
}
