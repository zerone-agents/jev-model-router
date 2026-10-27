package management_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestStatusAutomaticEffortReadiness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		levels   []string
		enabled  bool
		decision bool
		want     bool
	}{
		{"legacy sole model", nil, true, false, true},
		{"fixed effort", []string{"high"}, true, false, true},
		{"effort choice needs Jev", []string{"low", "high"}, true, false, false},
		{"effort choice with Jev", []string{"low", "high"}, true, true, true},
		{"disabled model", []string{"low", "high"}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, store, _ := checksFixture()
			store.s.Models[0].Enabled = tc.enabled
			if tc.levels != nil {
				store.s.Models[0].AutoEffort = &routing.AutoEffortConfig{Allowed: tc.levels}
			}
			if tc.decision {
				store.s.Decision.Model = "jev"
			}
			service := management.New(store, nil)
			result, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "status.get", Input: json.RawMessage("{}")})
			if err != nil {
				t.Fatal(err)
			}
			var status struct{ Ready bool }
			if err := json.Unmarshal(result.Data, &status); err != nil {
				t.Fatal(err)
			}
			if status.Ready != tc.want {
				t.Fatalf("ready=%v, want %v", status.Ready, tc.want)
			}
			if tc.enabled && !tc.decision {
				_, err := checks.Inspect(context.Background(), routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}})
				if tc.want && err != nil {
					t.Fatal(err)
				}
				var routingError *routing.Error
				if !tc.want && (!errors.As(err, &routingError) || routingError.Code != "config_missing") {
					t.Fatalf("expected missing decision configuration, got %v", err)
				}
			}
		})
	}
}
