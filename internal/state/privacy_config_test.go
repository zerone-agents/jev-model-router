package state_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

func TestPrivacyConfigurationRequiresLocalDecision(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	initial := snap(t, s)
	body, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Privacy struct {
			Enabled bool   `json:"enabled"`
			Prompt  string `json:"prompt"`
		} `json:"privacy"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Privacy.Enabled || snapshot.Privacy.Prompt == "" {
		t.Fatal("privacy must default disabled with a prompt")
	}
	version := initial.Version
	enable := management.Call{CapabilityID: "privacy.put", Input: json.RawMessage(`{"enabled":true}`), ExpectedVersion: &version, IdempotencyKey: "enable"}
	if _, err := s.Apply(ctx, "settings", enable, nil); err == nil {
		t.Fatal("enabled privacy without local decision")
	}
	if snap(t, s).Version != version {
		t.Fatal("rejected configuration changed version")
	}
}

func TestPrivacyConfigurationAtomicLocalityAndReplay(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	v := snap(t, s).Version
	decision := management.Call{CapabilityID: "decision.put", Input: json.RawMessage(`{"base_url":"http://127.0.0.1:8000","model":"local","secret_ref":"env:KEY","location":"local"}`), ExpectedVersion: &v, IdempotencyKey: "decision"}
	if _, err := s.Apply(ctx, "settings", decision, nil); err != nil {
		t.Fatal(err)
	}
	v = snap(t, s).Version
	enable := management.Call{CapabilityID: "privacy.put", Input: json.RawMessage(`{"enabled":true}`), ExpectedVersion: &v, IdempotencyKey: "enable"}
	if _, err := s.Apply(ctx, "settings", enable, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, "settings", enable, nil); err != nil {
		t.Fatalf("receipt replay: %v", err)
	}
	current := snap(t, s)
	if !current.Privacy.Enabled || current.Version != v+1 {
		t.Fatalf("bad snapshot: %+v", current)
	}
	v = current.Version
	cloud := management.Call{CapabilityID: "decision.put", Input: json.RawMessage(`{"base_url":"https://example.com","model":"cloud","secret_ref":"env:KEY","location":"cloud"}`), ExpectedVersion: &v, IdempotencyKey: "cloud"}
	if _, err := s.Apply(ctx, "settings", cloud, nil); err == nil {
		t.Fatal("allowed cloud decision while privacy enabled")
	}
	if got := snap(t, s); got.Version != v || got.Decision.Location != "local" {
		t.Fatal("failed write changed configuration")
	}
}

func TestPrivacyManagementContractAndPrompt(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	svc := management.New(s, nil)
	call := func(id, input, key string, version *int64) management.Result {
		t.Helper()
		result, err := svc.Execute(ctx, "settings", management.Call{CapabilityID: id, Input: json.RawMessage(input), ExpectedVersion: version, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	initial := call("privacy.get", `{}`, "", nil)
	var state struct {
		Version int64 `json:"version"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.Unmarshal(initial.Data, &state); err != nil || state.Enabled {
		t.Fatalf("initial: %s %v", initial.Data, err)
	}
	result := call("privacy.prompt.put", `{"text":"Treat credentials as sensitive."}`, "prompt", &state.Version)
	if !result.OK {
		t.Fatalf("write: %+v", result)
	}
	current := call("privacy.prompt.get", `{}`, "", nil)
	var prompt struct {
		Text    string `json:"text"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal(current.Data, &prompt); err != nil || prompt.Text != "Treat credentials as sensitive." || prompt.Version != state.Version+1 {
		t.Fatalf("read: %s %v", current.Data, err)
	}
}

func TestPrivacyMigrationPreservesLegacyReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	s, err := state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	v := snap(t, s).Version
	original := call(v, "legacy-key")
	receipt, err := s.Apply(ctx, "settings", original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"ALTER TABLE config_meta DROP COLUMN privacy_enabled", "ALTER TABLE config_meta DROP COLUMN privacy_prompt", "DELETE FROM schema_migrations WHERE name='007_privacy.sql'"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = state.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	current := snap(t, s)
	if current.Privacy.Enabled || current.Privacy.Prompt == "" {
		t.Fatalf("bad migrated defaults: %+v", current.Privacy)
	}
	replay, err := s.Apply(ctx, "settings", original, nil)
	if err != nil || replay.Meta.OperationID != receipt.Meta.OperationID {
		t.Fatalf("legacy receipt not replayed: %+v %v", replay, err)
	}
}
