package routing

import "context"

type Executor struct{ Generator Generator }

func (e *Executor) Complete(ctx context.Context, p Plan, r Request) (Completion, error) {
	return e.Generator.Complete(ctx, p.Target, WithEffectiveEffort(r, p.EffectiveEffort))
}
func (e *Executor) Stream(ctx context.Context, p Plan, r Request) (EventStream, error) {
	return e.Generator.Stream(ctx, p.Target, WithEffectiveEffort(r, p.EffectiveEffort))
}
