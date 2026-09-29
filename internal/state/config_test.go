package state_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func call(v int64, key string) management.Call {
	return management.Call{CapabilityID: "providers.put", Input: json.RawMessage(`{"id":"p","base_url":"https://example.com/v1","secret_ref":"env:KEY"}`), ExpectedVersion: &v, IdempotencyKey: key}
}
func open(t *testing.T) *state.Store {
	t.Helper()
	s, e := state.Open(filepath.Join(t.TempDir(), "db.sqlite"), time.Now)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func snap(t *testing.T, s *state.Store) routing.Snapshot {
	t.Helper()
	v, e := s.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestVersionConflictIsNoOp(t *testing.T) {
	s := open(t)
	before := snap(t, s)
	_, e := s.Apply(context.Background(), "settings", call(before.Version+1, "key"), nil)
	if e == nil || snap(t, s).Version != before.Version {
		t.Fatal("conflict changed state")
	}
}
func TestRejectedAutoLeavesVersionUnchanged(t *testing.T) {
	s := open(t)
	v := snap(t, s).Version
	_, e := s.Apply(context.Background(), "settings", call(v, "provider"), nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, enabled := range []bool{false, true} {
		before := snap(t, s)
		body, _ := json.Marshal(routing.Model{ID: "auto", ProviderID: "p", UpstreamName: "x", Enabled: enabled, Location: "cloud", Capabilities: routing.Capabilities{ContextLimit: 1000}})
		c := management.Call{CapabilityID: "models.put", Input: body, ExpectedVersion: &before.Version, IdempotencyKey: "invalid"}
		if _, e = s.Apply(context.Background(), "settings", c, nil); e == nil || snap(t, s).Version != before.Version {
			t.Fatal("auto mutated state")
		}
	}
}
func TestSameKeyDifferentInput(t *testing.T) {
	s := open(t)
	c := call(snap(t, s).Version, "key")
	if _, e := s.Apply(context.Background(), "settings", c, nil); e != nil {
		t.Fatal(e)
	}
	c.Input = json.RawMessage(`{"id":"q","base_url":"https://example.com","secret_ref":"env:KEY"}`)
	if _, e := s.Apply(context.Background(), "settings", c, nil); e == nil {
		t.Fatal("key reused with changed input")
	}
}
func TestValidationFailureDoesNotConsumeKey(t *testing.T) {
	s := open(t)
	c := call(snap(t, s).Version, "key")
	good := c.Input
	c.Input = json.RawMessage(`{"id":"p"}`)
	if _, e := s.Apply(context.Background(), "settings", c, nil); e == nil {
		t.Fatal("invalid accepted")
	}
	c.Input = good
	if _, e := s.Apply(context.Background(), "settings", c, nil); e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentReplay(t *testing.T) {
	s := open(t)
	before := snap(t, s)
	c := call(before.Version, "same")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Apply(context.Background(), "settings", c, nil)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- r.Meta.OperationID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for x := range ids {
		if id != "" && id != x {
			t.Fatal("multiple operations")
		}
		id = x
	}
	if snap(t, s).Version != before.Version+1 {
		t.Fatal("write repeated")
	}
}
func TestReplayAfterReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	s, e := state.Open(p, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	c := call(snap(t, s).Version, "same")
	first, e := s.Apply(context.Background(), "settings", c, nil)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = state.Open(p, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Apply(context.Background(), "settings", c, func(routing.Snapshot) error { t.Fatal("replay revalidated"); return nil })
	if e != nil || first.Meta.OperationID != r.Meta.OperationID {
		t.Fatal("replay lost")
	}
}
func TestExpiry24Hours(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	s, e := state.Open(filepath.Join(t.TempDir(), "db"), func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := call(snap(t, s).Version, "key")
	if _, e = s.Apply(context.Background(), "settings", c, nil); e != nil {
		t.Fatal(e)
	}
	now = now.Add(24 * time.Hour)
	if _, e = s.Apply(context.Background(), "settings", c, nil); e == nil {
		t.Fatal("expired result replayed despite stale version")
	}
}
func TestCommitCrashBoundary(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "db")
			s, e := state.Open(p, time.Now)
			if e != nil {
				t.Fatal(e)
			}
			v := snap(t, s).Version
			s.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
			cmd.Env = append(os.Environ(), "ROUTER_TEST_DB="+p, "ROUTER_TEST_CRASH="+stage)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("helper %v %s", e, out)
			}
			s, e = state.Open(p, time.Now)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			after := snap(t, s)
			expected := v
			if stage == "after" {
				expected++
			}
			if after.Version != expected {
				t.Fatal("non-atomic crash")
			}
			r, e := s.Apply(context.Background(), "settings", call(v, "crash"), nil)
			if e != nil || !r.OK || snap(t, s).Version != v+1 {
				t.Fatal("crash retry wrong")
			}
		})
	}
}
func TestCrashHelper(t *testing.T) {
	p := os.Getenv("ROUTER_TEST_DB")
	if p == "" {
		return
	}
	s, e := state.Open(p, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	v := snap(t, s).Version
	_, e = s.Apply(context.Background(), "settings", call(v, "crash"), func(routing.Snapshot) error {
		if os.Getenv("ROUTER_TEST_CRASH") == "before" {
			os.Exit(0)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	os.Exit(0)
}

func TestPromptUnicodeSchemaMatchesSave(t *testing.T) {
	s := open(t)
	for _, n := range []int{6000, 16384, 16385} {
		before := snap(t, s)
		text := strings.Repeat("中", n)
		body, _ := json.Marshal(map[string]string{"text": text})
		schemaErr := contracts.Validate("prompt.put", body)
		_, saveErr := s.Apply(context.Background(), "settings", management.Call{CapabilityID: "prompt.put", Input: body, ExpectedVersion: &before.Version, IdempotencyKey: fmt.Sprint(n)}, nil)
		if (schemaErr == nil) != (saveErr == nil) {
			t.Fatalf("%d chars: schema=%v save=%v", n, schemaErr, saveErr)
		}
		after := snap(t, s)
		if n <= 16384 && (saveErr != nil || after.Prompt != text) {
			t.Fatal("valid Unicode prompt rejected")
		}
		if n > 16384 && (saveErr == nil || after.Version != before.Version) {
			t.Fatal("oversize prompt modified config")
		}
	}
}
