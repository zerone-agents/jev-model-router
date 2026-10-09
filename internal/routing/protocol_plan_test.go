package routing

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMixedProtocolOutputReserve(t *testing.T) {
	s, r := planFixture()
	s.Providers = append(s.Providers, Provider{ID: "native", Protocol: ProtocolAnthropic})
	s.Models[0].ProviderID = "native"
	s.Models[0].Capabilities.ContextLimit = 20000
	s.Models[1].Capabilities.ContextLimit = 20000
	for i := 0; i < 2; i++ {
		p, e := (&Planner{}).Plan(context.Background(), s, r)
		if e != nil {
			t.Fatal(e)
		}
		if p.ModelID != "b" || len(p.CandidateIDs) != 1 || p.ContextEstimate.OutputReserve != 4096 {
			t.Fatalf("mixed: %+v", p)
		}
		s.Models[0], s.Models[1] = s.Models[1], s.Models[0]
	}
	s.Models = s.Models[:1]
	s.Models[0].Capabilities.ContextLimit = 100000
	p, e := (&Planner{}).Plan(context.Background(), s, r)
	if e != nil || p.ContextEstimate.OutputReserve != 65536 {
		t.Fatalf("native: %+v %v", p, e)
	}
	if len(r.Options) != 0 {
		t.Fatal("request mutated")
	}
	r.Options = map[string]json.RawMessage{"max_completion_tokens": json.RawMessage(`128`)}
	p, e = (&Planner{}).Plan(context.Background(), s, r)
	if e != nil || p.ContextEstimate.OutputReserve != 128 {
		t.Fatalf("limit: %+v %v", p, e)
	}
}
func TestProtocolChecksBeforeDecision(t *testing.T) {
	s, r := planFixture()
	_, e := (&Planner{PrepareCheck: func(Request) func(Target) error {
		return func(Target) error { return Fail("internal_error", "conversion failed") }
	}}).Plan(context.Background(), s, r)
	if e == nil || e.Error() != "internal_error: conversion failed" {
		t.Fatalf("swallowed internal failure: %v", e)
	}
}
