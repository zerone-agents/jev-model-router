package state_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"path/filepath"
	"reflect"
	"strings"
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
	m := routing.Model{ID: "m", ProviderID: "p", UpstreamName: "m", Description: "test", Location: "cloud", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 4096, Reasoning: []routing.ReasoningCombination{{Effort: "low"}, {Effort: "xhigh"}}}}
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
	if cfg.Models[0].Capabilities.Reasoning[1].Effort != "xhigh" {
		t.Fatal("configuration lost")
	}
	c = call(cfg.Version, "protocol-change")
	c.Input = json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY","protocol":"anthropic"}`)
	if _, err = s.Apply(ctx, "settings", c, nil); err != nil {
		t.Fatal(err)
	}
	if levels := routing.AutomaticEffortLevels(snap(t, s).Models[0], routing.ProtocolAnthropic); len(levels) != 1 || levels[0] != "low" {
		t.Fatal("unsupported level emitted", levels)
	}
}

func TestLegacyAutoEffortIgnoredOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	s, err := state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	v := snap(t, s).Version
	if _, err = s.Apply(ctx, "settings", call(v, "provider"), nil); err != nil {
		t.Fatal(err)
	}
	v = snap(t, s).Version
	raw := json.RawMessage(`{"id":"m","provider_id":"p","upstream_name":"up","description":"keep","location":"cloud","enabled":false,"capabilities":{"context_limit":4096,"tools":false,"images":false,"structured_output":false,"reasoning":[{"reasoning_effort":"low"}]}}`)
	if _, err = s.Apply(ctx, "settings", management.Call{CapabilityID: "models.put", Input: raw, ExpectedVersion: &v, IdempotencyKey: "model"}, nil); err != nil {
		t.Fatal(err)
	}
	before := snap(t, s)
	s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.TrimSuffix(string(raw), "}") + `,"auto_effort":{"allowed":["max"]}}`
	if _, err = db.Exec("UPDATE models SET body=? WHERE id='m'", legacy); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after := snap(t, s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("legacy load changed unrelated configuration")
	}
	if got := routing.AutomaticEffortLevels(after.Models[0], routing.ProtocolOpenAI); len(got) != 1 || got[0] != "low" {
		t.Fatal(got)
	}
	v = after.Version
	_, err = s.Apply(ctx, "settings", management.Call{CapabilityID: "models.put", Input: json.RawMessage(legacy), ExpectedVersion: &v, IdempotencyKey: "legacy-write"}, nil)
	if err == nil || !strings.Contains(err.Error(), "capabilities.reasoning") || snap(t, s).Version != v {
		t.Fatal("legacy write not safely rejected", err)
	}
}
