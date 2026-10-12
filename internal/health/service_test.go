package health

import (
	"context"
	"testing"
	"time"
)

type memoryStore struct{ records []Record }

func (*memoryStore) LoadHealthKey(context.Context) ([32]byte, error) { return [32]byte{1}, nil }
func (m *memoryStore) LoadHealth(context.Context) ([]Record, error)  { return m.records, nil }
func (*memoryStore) SaveHealth(context.Context, Record) error        { return nil }
func TestFailureWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	s := New(&memoryStore{}, DefaultPolicy(), func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if e := s.Observe(context.Background(), "m", s.Begin("m"), Outcome{Kind: Transient}); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.Get("m")
	if e != nil || r.State != Isolated || (r.NextProbe.Sub(now) < 30*time.Second || r.NextProbe.Sub(now) > 33*time.Second) {
		t.Fatalf("not isolated: %+v %v", r, e)
	}
	if e = s.Observe(context.Background(), "m", s.Begin("m"), Outcome{Success: true}); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Get("m")
	if r.State != Healthy || len(r.FailureTimes) != 0 {
		t.Fatal("success did not reset")
	}
}
func TestObservationOrder(t *testing.T) {
	s := New(&memoryStore{}, DefaultPolicy(), time.Now)
	old := s.Begin("m")
	latest := s.Begin("m")
	s.Observe(context.Background(), "m", latest, Outcome{Kind: Permanent})
	s.Observe(context.Background(), "m", old, Outcome{Success: true})
	r, _ := s.Get("m")
	if r.State != Isolated {
		t.Fatal("late old success overwrote newer failure")
	}
}
func TestRetryAfterBounds(t *testing.T) {
	now := time.Now()
	for _, v := range []string{"-1", "wat", "99999999999999999999999"} {
		if !ParseRetryAfter(v, now).IsZero() {
			t.Fatal("invalid retry accepted")
		}
	}
	if ParseRetryAfter("3600", now).Sub(now) != time.Hour {
		t.Fatal("long retry clipped")
	}
}
func TestPersistenceWarningSurvivesOtherSuccess(t *testing.T) {
	m := &failingStore{}
	s := New(m, DefaultPolicy(), time.Now)
	m.fail = true
	if s.Observe(context.Background(), "a", s.Begin("a"), Outcome{Kind: Permanent}) == nil {
		t.Fatal("write unexpectedly passed")
	}
	m.fail = false
	s.Observe(context.Background(), "b", s.Begin("b"), Outcome{Success: true})
	if !s.Warning() {
		t.Fatal("lost dirty target warning")
	}
}

type failingStore struct {
	memoryStore
	fail bool
}

func (m *failingStore) SaveHealth(context.Context, Record) error {
	if m.fail {
		return ErrUnavailable
	}
	return nil
}
func TestIgnoredProbeBackoffAndLongRetry(t *testing.T) {
	now := time.Now()
	s := New(&memoryStore{}, DefaultPolicy(), func() time.Time { return now })
	s.Observe(context.Background(), "m", s.Begin("m"), Outcome{Kind: Limited, RetryAt: now.Add(time.Hour)})
	r, _ := s.Get("m")
	if r.NextProbe.Before(now.Add(time.Hour)) {
		t.Fatal("Retry-After shortened")
	}
	seq := r.AppliedSequence
	s.DeferProbe(context.Background(), "m", seq)
	r, _ = s.Get("m")
	if r.NextProbe.Before(now.Add(time.Hour)) {
		t.Fatal("ignored probe shortened prior Retry-After")
	}
}
