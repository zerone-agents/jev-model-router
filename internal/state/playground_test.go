package state_test

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaygroundAtomicAdmission(t *testing.T) {
	st, e := state.Open(filepath.Join(t.TempDir(), "test.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	l := playground.DefaultLimits()
	l.DailyRequests = 1
	now := time.Date(2026, 10, 10, 23, 59, 0, 0, time.UTC)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := st.AdmitPlayground(context.Background(), "s", now, l); e == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successes %d", successes.Load())
	}
	q, e := st.PlaygroundQuota(context.Background(), "s", now, l)
	if e != nil || q.SessionRemaining != 5 || q.DayRemaining != 0 {
		t.Fatalf("quota %+v %v", q, e)
	}
}
func TestPlaygroundWindowAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, e := state.Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	l := playground.DefaultLimits()
	l.SessionRPM = 1
	now := time.Date(2026, 10, 10, 23, 58, 0, 0, time.UTC)
	if _, e = st.AdmitPlayground(context.Background(), "s", now, l); e != nil {
		t.Fatal(e)
	}
	st.Close()
	st, e = state.Open(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	_, e = st.AdmitPlayground(context.Background(), "s", now.Add(59999*time.Millisecond), l)
	var limit *playground.LimitError
	if !errors.As(e, &limit) {
		t.Fatalf("expected limited: %v", e)
	}
	if _, e = st.AdmitPlayground(context.Background(), "s", now.Add(time.Minute), l); e != nil {
		t.Fatal(e)
	}
	q, _ := st.PlaygroundQuota(context.Background(), "new-session", now.Add(time.Minute), l)
	if q.DayRemaining != 198 || q.InstanceRemaining != 19 {
		t.Fatalf("reset via login: %+v", q)
	}
	q, _ = st.PlaygroundQuota(context.Background(), "s", now.Add(2*time.Minute), l)
	if q.DayRemaining != 200 {
		t.Fatalf("UTC day: %+v", q)
	}
}
func TestPlaygroundReleaseAndStorageFailure(t *testing.T) {
	st, e := state.Open(filepath.Join(t.TempDir(), "test.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	l := playground.DefaultLimits()
	l.InstanceConcurrency = 1
	s := playground.NewService(st, l, time.Now)
	lease, e := s.Acquire(context.Background(), "s")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Acquire(context.Background(), "other"); e == nil {
		t.Fatal("concurrent admitted")
	}
	lease.Release()
	lease.Release()
	lease, e = s.Acquire(context.Background(), "other")
	if e != nil {
		t.Fatal(e)
	}
	lease.Release()
	st.Close()
	if _, e = s.Acquire(context.Background(), "s"); e == nil {
		t.Fatal("storage failure admitted")
	}
}
