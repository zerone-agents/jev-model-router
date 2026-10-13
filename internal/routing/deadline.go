package routing

import (
	"context"
	"errors"
	"time"
)

// ErrRequestBudgetExhausted is a caller-owned limit, never a target failure.
var ErrRequestBudgetExhausted = errors.New("request_budget_exhausted")

func WithStageTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if total, ok := parent.Deadline(); ok && !deadline.Before(total) {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, deadline)
}

// WithRequestBudget retains values and cancellation while assigning an explicit
// cause even when an inherited deadline expires first.
func WithRequestBudget(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if d, ok := parent.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	base, stop := context.WithCancelCause(context.WithoutCancel(parent))
	ctx, cancel := context.WithDeadlineCause(base, deadline, ErrRequestBudgetExhausted)
	propagate := func() {
		cause := context.Cause(parent)
		if errors.Is(cause, context.DeadlineExceeded) {
			cause = ErrRequestBudgetExhausted
		}
		stop(cause)
	}
	off := context.AfterFunc(parent, propagate)
	if parent.Err() != nil {
		propagate()
	}
	return ctx, func() { off(); cancel(); stop(context.Canceled) }
}
