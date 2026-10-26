package state_test

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"path/filepath"
	"testing"
	"time"
)

func TestAutoEffortPersistenceAndProtocolChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	v := snap(t, s).Version
	if _, err = s.Apply(ctx, "settings", call(v, "provider"), nil); err != nil {
		t.Fatal(err)
	}
	m := routing.Model{ID: "m", ProviderID: "p", UpstreamName: "m", Description: "test", Location: "cloud", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 4096}, AutoEffort: &routing.AutoEffortConfig{Allowed: []string{"low", "xhigh"}}}
	b, _ := json.Marshal(m)
	v = snap(t, s).Version
	c := management.Call{CapabilityID: "models.put", Input: b, ExpectedVersion: &v, IdempotencyKey: "model"}
	if _, err = s.Apply(ctx, "settings", c, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(ctx, "settings", c, nil); err != nil {
		t.Fatal("replay", err)
	}
	s.Close()
	s, err = state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := snap(t, s)
	if cfg.Models[0].AutoEffort.Allowed[1] != "xhigh" {
		t.Fatal("configuration lost")
	}
	c = call(cfg.Version, "protocol-change")
	c.Input = json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY","protocol":"anthropic"}`)
	if _, err = s.Apply(ctx, "settings", c, nil); err == nil {
		t.Fatal("incompatible protocol change accepted")
	}
	if snap(t, s).Version != cfg.Version {
		t.Fatal("failed change mutated version")
	}
}
