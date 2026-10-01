package state_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestReasoningCapabilitiesSurviveConfiguration(t *testing.T) {
	s := open(t)
	if _, err := s.Apply(context.Background(), "settings", call(snap(t, s).Version, "provider"), nil); err != nil {
		t.Fatal(err)
	}
	want := routing.Model{ID: "m", ProviderID: "p", UpstreamName: "upstream", Location: "cloud", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 8192, Reasoning: []routing.ReasoningCombination{{EnableThinking: new(false)}, {Effort: "high"}, {EnableThinking: new(true), Effort: "high"}}}}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	version := snap(t, s).Version
	if _, err := s.Apply(context.Background(), "settings", management.Call{CapabilityID: "models.put", Input: body, ExpectedVersion: &version, IdempotencyKey: "reasoning"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := snap(t, s).Models[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stored capabilities changed: %+v", got.Capabilities)
	}
	svc := management.New(s, nil)
	result, err := svc.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.get", Input: json.RawMessage(`{"id":"m"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Resource routing.Model }
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Resource.Capabilities, want.Capabilities) {
		t.Fatalf("management response lost capabilities: %s", result.Data)
	}
}
