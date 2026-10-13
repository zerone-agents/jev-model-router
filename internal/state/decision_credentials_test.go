package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
)

func decisionKeyCall(v int64, key, idem string) management.Call {
	b, _ := json.Marshal(map[string]string{"base_url": "https://decision.example.com", "path": "/api/v1/decisions", "model": "provider-model-id", "api_key": key})
	return management.Call{CapabilityID: "decision.put", Input: b, ExpectedVersion: &v, IdempotencyKey: idem}
}
func TestDecisionManagedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := OpenWithCipher(path, time.Now, cipherForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	first := decisionKeyCall(1, "decision-old-secret", "create")
	r, err := s.Apply(ctx, "settings", first, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := managedRef(t, r)
	if strings.Contains(string(r.Data), "decision-old-secret") {
		t.Fatal("key leaked")
	}
	r, err = s.Apply(ctx, "settings", decisionKeyCall(2, "decision-new-secret", "rotate"), nil)
	if err != nil {
		t.Fatal(err)
	}
	newer := managedRef(t, r)
	if newer == old {
		t.Fatal("mutable revision")
	}
	replay, err := s.Apply(ctx, "settings", first, nil)
	if err != nil || managedRef(t, replay) != old {
		t.Fatal("replay failed", err)
	}
	if _, err = s.Apply(ctx, "settings", decisionKeyCall(1, "different", "create"), nil); err == nil {
		t.Fatal("idempotency conflict missing")
	}
	// Both ownership directions are enforced, without publishing a new version.
	c := managedCall(3, "p", "provider-secret", "provider")
	r, err = s.Apply(ctx, "settings", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	providerRef := managedRef(t, r)
	for _, tc := range []struct {
		cap   string
		input map[string]string
	}{
		{"decision.put", map[string]string{"base_url": "https://example.com", "model": "jev", "secret_ref": providerRef}},
		{"providers.put", map[string]string{"id": "p", "base_url": "https://example.com", "secret_ref": newer}},
	} {
		b, _ := json.Marshal(tc.input)
		v := int64(4)
		if _, err = s.Apply(ctx, "settings", management.Call{CapabilityID: tc.cap, Input: b, ExpectedVersion: &v, IdempotencyKey: "wrong-owner"}, nil); err == nil {
			t.Fatal("cross-owner ref accepted")
		}
	}
	s.Close()
	s, err = OpenWithCipher(path, time.Now, cipherForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{old: "decision-old-secret", newer: "decision-new-secret"} {
		key, err := s.ResolveManaged(ref)
		if err != nil || string(key) != want {
			t.Fatal("restart/rotation resolution failed", err)
		}
	}
	snap, err := s.Snapshot(ctx)
	if err != nil || snap.Version != 4 || snap.Decision.SecretRef != newer || snap.Decision.Path != "/api/v1/decisions" {
		t.Fatal("snapshot mismatch", err)
	}
	rows, err := s.db.Query("SELECT digest,result FROM idempotency")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var digest, result string
		if err := rows.Scan(&digest, &result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result, "-secret") {
			t.Fatal("receipt leaked key")
		}
	}
}
func TestDecisionKeyWithoutMasterIsRejected(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Apply(context.Background(), "settings", decisionKeyCall(1, "secret", "key"), nil); err == nil {
		t.Fatal("missing master accepted")
	}
	snap, _ := s.Snapshot(context.Background())
	if snap.Version != 1 {
		t.Fatal("failed write changed version")
	}
}
