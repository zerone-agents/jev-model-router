package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBudgetExhaustionHealthNeutral(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), time.Millisecond, ErrRequestBudgetExhausted)
	defer cancel()
	child, stop := WithStageTimeout(ctx, time.Second)
	defer stop()
	<-child.Done()
	if !errors.Is(context.Cause(child), ErrRequestBudgetExhausted) {
		t.Fatal(context.Cause(child))
	}
	if o := HealthOutcome(context.Cause(child)); o.Success || o.Kind != "" {
		t.Fatal(o)
	}
}
func TestStageDeadlineClassification(t *testing.T) {
	ctx, cancel := WithStageTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-ctx.Done()
	if HealthOutcome(context.Cause(ctx)).Kind == "" {
		t.Fatal("stage timeout ignored")
	}
}
