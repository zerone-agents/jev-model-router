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
