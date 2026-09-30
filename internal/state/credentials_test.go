package state

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/credential"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func cipherForTest(t *testing.T) *credential.Cipher {
	t.Helper()
	c, e := credential.New(strings.Repeat("ab", 32))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func managedCall(v int64, id, key, idem string) management.Call {
	b, _ := json.Marshal(map[string]string{"id": id, "base_url": "https://example.com/v1", "api_key": key})
	return management.Call{CapabilityID: "providers.put", Input: b, ExpectedVersion: &v, IdempotencyKey: idem}
}
func managedRef(t *testing.T, r management.Result) string {
	t.Helper()
	var d struct{ Resource routing.Provider }
	if e := json.Unmarshal(r.Data, &d); e != nil {
		t.Fatal(e)
	}
	return d.Resource.SecretRef
}
func TestManagedLifecycleAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	c := cipherForTest(t)
	s, e := OpenWithCipher(path, time.Now, c)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	first := managedCall(1, "p", "sentinel-old-key", "create")
	r, e := s.Apply(ctx, "settings", first, nil)
	if e != nil {
		t.Fatal(e)
	}
	old := managedRef(t, r)
	r2, e := s.Apply(ctx, "settings", managedCall(2, "p", "sentinel-new-key", "rotate"), nil)
	if e != nil {
		t.Fatal(e)
	}
	newRef := managedRef(t, r2)
	for ref, want := range map[string]string{old: "sentinel-old-key", newRef: "sentinel-new-key"} {
		v, e := s.ResolveManaged(ref)
		if e != nil || string(v) != want {
			t.Fatal("wrong revision", e)
		}
	}
	replay, e := s.Apply(ctx, "settings", first, nil)
	if e != nil || replay.Meta.OperationID != r.Meta.OperationID {
		t.Fatal("replay changed", e)
	}
	if _, e = s.Apply(ctx, "settings", managedCall(1, "p", "changed-secret", "create"), nil); e == nil {
		t.Fatal("changed replay accepted")
	}
	for _, table := range []string{"providers", "managed_credentials", "idempotency"} {
		rows, e := s.db.Query("SELECT * FROM " + table)
		if e != nil {
			t.Fatal(e)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if e = rows.Scan(ptrs...); e != nil {
				t.Fatal(e)
			}
			b, _ := json.Marshal(vals)
			if strings.Contains(string(b), "sentinel-") {
				t.Fatal("plaintext in SQL", table)
			}
		}
		rows.Close()
	}
	s.Close()
	for _, key := range []*credential.Cipher{nil, mustWrongCipher(t)} {
		if other, e := OpenWithCipher(path, time.Now, key); e == nil {
			other.Close()
			t.Fatal("missing/wrong master accepted")
		}
	}
	s, e = OpenWithCipher(path, time.Now, c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	replay, e = s.Apply(ctx, "settings", first, nil)
	if e != nil || replay.Meta.OperationID != r.Meta.OperationID {
		t.Fatal("restart replay lost")
	}
	v := int64(3)
	del := management.Call{CapabilityID: "providers.delete", Input: json.RawMessage(`{"id":"p"}`), ExpectedVersion: &v, IdempotencyKey: "delete"}
	if _, e = s.Apply(ctx, "settings", del, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, "settings", managedCall(4, "p", "recreated-key", "recreate"), nil); e != nil {
		t.Fatal(e)
	}
	if key, e := s.ResolveManaged(old); e != nil || string(key) != "sentinel-old-key" {
		t.Fatal("in-flight revision removed")
	}
}
func mustWrongCipher(t *testing.T) *credential.Cipher {
	t.Helper()
	c, e := credential.New(strings.Repeat("cd", 32))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestManagedRollbackAndOwnership(t *testing.T) {
	s, e := OpenWithCipher(filepath.Join(t.TempDir(), "db"), time.Now, cipherForTest(t))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	c := managedCall(1, "p", "marker-key", "k")
	if _, e = s.Apply(ctx, "settings", c, func(routing.Snapshot) error { return errors.New("reject") }); e == nil {
		t.Fatal("validation accepted")
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM managed_credentials").Scan(&n)
	if n != 0 {
		t.Fatal("orphan secret after rollback")
	}
	r, e := s.Apply(ctx, "settings", c, nil)
	if e != nil {
		t.Fatal(e)
	}
	ref := managedRef(t, r)
	for _, owner := range []string{"q", "p"} {
		b, _ := json.Marshal(routing.Provider{ID: owner, BaseURL: "https://example.com", SecretRef: ref})
		v := int64(2)
		_, e = s.Apply(ctx, "settings", management.Call{CapabilityID: "providers.put", Input: b, ExpectedVersion: &v, IdempotencyKey: owner}, nil)
		if (e == nil) != (owner == "p") {
			t.Fatal("ownership incorrect", e)
		}
	}
	if _, e = s.Apply(ctx, "settings", managedCall(1, "p", "bad-version-key", "stale"), nil); e == nil {
		t.Fatal("stale write accepted")
	}
	s.db.QueryRow("SELECT count(*) FROM managed_credentials").Scan(&n)
	if n != 1 {
		t.Fatal("failed write created revision")
	}
}
func TestManagedMissingCipherAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(context.Background(), "settings", managedCall(1, "p", "secret", "k"), nil); e == nil {
		t.Fatal("plaintext fallback")
	}
	s.Close()
	s, e = OpenWithCipher(path, time.Now, cipherForTest(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(context.Background(), "settings", managedCall(1, "p", "secret", "k"), nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE managed_credentials SET envelope='v1:AA=='"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if s, e = OpenWithCipher(path, time.Now, cipherForTest(t)); e == nil {
		s.Close()
		t.Fatal("corruption accepted")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "sentinel-old-key") {
		t.Fatal("plaintext database")
	}
}
func TestManagedConcurrentRotation(t *testing.T) {
	s, e := OpenWithCipher(filepath.Join(t.TempDir(), "db"), time.Now, cipherForTest(t))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Apply(context.Background(), "settings", managedCall(1, "p", id, id), nil)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("concurrent stale writes both committed")
	}
}
