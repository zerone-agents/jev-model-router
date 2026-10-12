package state

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"path/filepath"
	"testing"
)

func TestHealthKeyRestartWithoutMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	s, e := Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.LoadHealthKey(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if key == [32]byte{} {
		t.Fatal("empty key")
	}
	s.Close()
	s, e = Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	again, e := s.LoadHealthKey(context.Background())
	if e != nil || key != again {
		t.Fatalf("key changed: %v", e)
	}
}
func TestHealthKeyMissingDoesNotRegenerate(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "r.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.LoadHealthKey(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("UPDATE health_meta SET fingerprint_key=NULL WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadHealthKey(context.Background()); e == nil {
		t.Fatal("missing initialized key regenerated")
	}
}

func TestHealthStateRestartAndConfigVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "r.db")
	s, e := Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadHealthKey(ctx); e != nil {
		t.Fatal(e)
	}
	r := health.Record{ID: "target", View: health.View{State: health.Isolated}, AppliedSequence: 42}
	if e = s.SaveHealth(ctx, r); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, e := s.LoadHealth(ctx)
	if e != nil || len(rows) != 1 || rows[0].State != health.Isolated || rows[0].AppliedSequence != 42 {
		t.Fatalf("restart: %+v %v", rows, e)
	}
	after, e := s.Snapshot(ctx)
	if e != nil || after.Version != before.Version {
		t.Fatal("health changed config version")
	}
}
func TestHealthDeletedKeyRowDoesNotRegenerate(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "r.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.LoadHealthKey(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("DELETE FROM health_meta"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LoadHealthKey(context.Background()); e == nil {
		t.Fatal("deleted initialized key regenerated")
	}
}
