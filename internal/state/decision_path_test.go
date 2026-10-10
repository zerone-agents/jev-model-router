package state_test

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"testing"
)

func TestDecisionPathReplacement(t *testing.T) {
	s := open(t)
	for i, path := range []string{"/api/v1/decisions", ""} {
		before := snap(t, s)
		input := map[string]string{"base_url": "https://example.com", "model": "jev", "secret_ref": "env:KEY"}
		if path != "" {
			input["path"] = path
		}
		b, _ := json.Marshal(input)
		_, err := s.Apply(context.Background(), "settings", management.Call{CapabilityID: "decision.put", Input: b, ExpectedVersion: &before.Version, IdempotencyKey: []string{"custom", "default"}[i]}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := snap(t, s).Decision.Path; got != path {
			t.Fatalf("path=%q want %q", got, path)
		}
	}
	for _, path := range []string{"https://evil.test", "//evil.test", "/v1?key=x", "/v1#fragment", "/../other", "/%2e%2e/other"} {
		before := snap(t, s)
		b, _ := json.Marshal(map[string]string{"base_url": "https://example.com", "model": "jev", "secret_ref": "env:KEY", "path": path})
		_, err := s.Apply(context.Background(), "settings", management.Call{CapabilityID: "decision.put", Input: b, ExpectedVersion: &before.Version, IdempotencyKey: "invalid"}, nil)
		if err == nil || snap(t, s).Version != before.Version {
			t.Fatalf("invalid path accepted: %s", path)
		}
	}
}
