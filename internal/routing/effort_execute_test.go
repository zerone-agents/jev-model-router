package routing

import (
	"context"
	"encoding/json"
	"testing"
)

type effortGenerator struct{ t *testing.T }

func (g effortGenerator) Complete(_ context.Context, _ Target, r Request) (Completion, error) {
	if ExplicitEffort(r) != "high" {
		g.t.Fatal("effort not applied")
	}
	r.Options["reasoning_effort"] = json.RawMessage(`"low"`)
	return Completion{}, nil
}
func (g effortGenerator) Stream(_ context.Context, _ Target, r Request) (EventStream, error) {
	if ExplicitEffort(r) != "high" {
		g.t.Fatal("stream effort not applied")
	}
	return nil, nil
}
func TestExecutorEffortCopy(t *testing.T) {
	_, r := planFixture()
	r.Options = map[string]json.RawMessage{}
	e := Executor{Generator: effortGenerator{t}}
	p := Plan{EffectiveEffort: "high"}
	e.Complete(context.Background(), p, r)
	e.Stream(context.Background(), p, r)
	if len(r.Options) != 0 {
		t.Fatal("caller mutated")
	}
}
