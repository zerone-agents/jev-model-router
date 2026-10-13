package routing

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRetryExcludesFailedModel(t *testing.T) {
	p := &Planner{}
	s := Snapshot{Models: []Model{{ID: "a", Enabled: true, ProviderID: "p", Capabilities: Capabilities{ContextLimit: 10000}}, {ID: "b", Enabled: true, ProviderID: "p", Capabilities: Capabilities{ContextLimit: 10000}}}, Providers: []Provider{{ID: "p"}}}
	r := Request{Model: "auto", Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	v, e := p.PlanWithOptions(context.Background(), s, r, nil, nil, PlanOptions{ExcludedModelIDs: []string{"a"}})
	if e != nil || v.ModelID != "b" || len(v.CandidateIDs) != 1 {
		t.Fatalf("%+v %v", v, e)
	}
}
