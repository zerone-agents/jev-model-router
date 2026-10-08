package state

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"path/filepath"
	"testing"
	"time"
)

func TestProviderProtocolPersistence(t *testing.T) {
	s, e := OpenWithCipher(filepath.Join(t.TempDir(), "db"), time.Now, cipherForTest(t))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	// Simulate a pre-protocol row without writing through today's DTO.
	if _, e = s.db.Exec(`INSERT INTO providers(id,body) VALUES('p','{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY"}')`); e != nil {
		t.Fatal(e)
	}
	cfg, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(cfg.Providers[0])
	var old map[string]any
	json.Unmarshal(b, &old)
	if old["protocol"] != "openai" || cfg.Version != 1 {
		t.Fatalf("legacy read: %s version %d", b, cfg.Version)
	}
	v := cfg.Version
	c := management.Call{CapabilityID: "providers.put", Input: json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","api_key":"secret","protocol":"anthropic"}`), ExpectedVersion: &v, IdempotencyKey: "native"}
	first, e := s.Apply(ctx, "settings", c, nil)
	if e != nil {
		t.Fatal(e)
	}
	cfg, _ = s.Snapshot(ctx)
	b, _ = json.Marshal(cfg.Providers[0])
	json.Unmarshal(b, &old)
	if old["protocol"] != "anthropic" {
		t.Fatalf("managed DTO lost protocol: %s", b)
	}
	replay, e := s.Apply(ctx, "settings", c, nil)
	if e != nil || replay.Meta.OperationID != first.Meta.OperationID {
		t.Fatal("replay", e)
	}
	for _, p := range []string{`""`, `null`, `"bad"`} {
		v = cfg.Version
		c.ExpectedVersion = &v
		c.IdempotencyKey = "bad" + p
		c.Input = json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY","protocol":` + p + `}`)
		if _, e = s.Apply(ctx, "settings", c, nil); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	v = cfg.Version
	c.IdempotencyKey = "default"
	c.Input = json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY"}`)
	if _, e = s.Apply(ctx, "settings", c, nil); e != nil {
		t.Fatal(e)
	}
	cfg, _ = s.Snapshot(ctx)
	b, _ = json.Marshal(cfg.Providers[0])
	json.Unmarshal(b, &old)
	if old["protocol"] != "openai" || cfg.Version != 3 {
		t.Fatalf("replacement: %s %d", b, cfg.Version)
	}
}
