package state

import (
	"context"
	"crypto/sha256"
	"github.com/zerone-agents/jev-model-router/internal/session"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSessionPersistenceRotationCapacity(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	key := sha256.Sum256([]byte("settings-A"))
	s, err := session.New(ctx, st, key, "origin", clock)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Login(ctx, "origin", "")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	s, err = session.New(ctx, st, key, "origin", clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ctx, "origin", first.Token); err != nil {
		t.Fatal("restart", err)
	}
	var version int
	st.db.QueryRow("SELECT version FROM config_meta").Scan(&version)
	if version != 1 {
		t.Fatal(version)
	}
	var hash string
	st.db.QueryRow("SELECT hash FROM dashboard_sessions").Scan(&hash)
	if hash == first.Token || len(hash) != 64 {
		t.Fatal("plaintext")
	}
	for i := 1; i < 128; i++ {
		if _, err = s.Login(ctx, "origin", ""); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err = s.Login(ctx, "origin", ""); err == nil {
		t.Fatal("capacity not enforced")
	}
	replacement, err := s.Login(ctx, "origin", first.Token)
	if err != nil {
		t.Fatal("replacement", err)
	}
	if _, err = s.Status(ctx, "origin", first.Token); err == nil {
		t.Fatal("old retained")
	}
	if err = s.Logout(ctx, "origin", replacement.Token, replacement.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Login(ctx, "origin", ""); err != nil {
		t.Fatal("capacity not freed")
	}
	_, err = session.New(ctx, st, sha256.Sum256([]byte("settings-B")), "origin", clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Login(ctx, "origin", ""); err == nil {
		t.Fatal("stale generation insert")
	}
	s, err = session.New(ctx, st, key, "origin", clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ctx, "origin", first.Token); err == nil {
		t.Fatal("A-B-A revival")
	}
	fresh, err := s.Login(ctx, "origin", "")
	if err != nil {
		t.Fatal(err)
	}
	s, err = session.New(ctx, st, key, "new-origin", clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ctx, "origin", fresh.Token); err == nil {
		t.Fatal("origin rotation")
	}
	now = now.Add(24 * time.Hour)
	if _, err = s.Login(ctx, "new-origin", ""); err != nil {
		t.Fatal(err)
	}
}
func TestSessionConcurrentBoundAndRollback(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := session.New(ctx, st, sha256.Sum256([]byte("key")), "origin", nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 140; i++ {
		wg.Go(func() { _, _ = s.Login(ctx, "origin", "") })
	}
	wg.Wait()
	var n int
	st.db.QueryRow("SELECT count(*) FROM dashboard_sessions").Scan(&n)
	if n != 128 {
		t.Fatal(n)
	}
	// Force insert failure after replacement deletion; rollback must retain old row.
	var r session.Record
	var created, expires int64
	st.db.QueryRow("SELECT hash,generation,origin,created_ns,expires_ns FROM dashboard_sessions LIMIT 1").Scan(&r.Hash, &r.Generation, &r.Origin, &created, &expires)
	r.CreatedAt = time.Unix(0, created)
	r.ExpiresAt = time.Unix(0, expires)
	_, err = st.db.Exec("CREATE TRIGGER reject_session BEFORE INSERT ON dashboard_sessions BEGIN SELECT RAISE(ABORT, 'test'); END")
	if err != nil {
		t.Fatal(err)
	}
	old := r.Hash
	r.Hash = "new-hash"
	if st.CreateSession(ctx, r, old) == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err = st.GetSession(ctx, old); err != nil {
		t.Fatal("rollback lost old session", err)
	}
}
