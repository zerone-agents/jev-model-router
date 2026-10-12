package health

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

type Service struct {
	dirty   map[TargetID]bool
	active  map[TargetID]int
	mu      sync.Mutex
	store   Store
	policy  Policy
	now     func() time.Time
	key     [32]byte
	err     error
	records map[TargetID]Record
	seq     uint64
}

func New(store Store, p Policy, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	s := &Service{store: store, policy: p, now: now, records: map[TargetID]Record{}, dirty: map[TargetID]bool{}, active: map[TargetID]int{}}
	s.key, s.err = store.LoadHealthKey(context.Background())
	if s.err != nil {
		return s
	}
	rows, e := store.LoadHealth(context.Background())
	if e != nil {
		s.err = e
		return s
	}
	for _, r := range rows {
		s.records[r.ID] = r
		if r.AppliedSequence > s.seq {
			s.seq = r.AppliedSequence
		}
	}
	return s
}
func (s *Service) Key() ([32]byte, error) { return s.key, s.err }
func (s *Service) Begin(id TargetID) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.active[id]++
	return s.seq
}
func clone(r Record) Record {
	r.FailureTimes = append([]time.Time(nil), r.FailureTimes...)
	if r.NextProbe != nil {
		v := *r.NextProbe
		r.NextProbe = &v
	}
	if r.LastSuccess != nil {
		v := *r.LastSuccess
		r.LastSuccess = &v
	}
	if r.LastFailure != nil {
		v := *r.LastFailure
		r.LastFailure = &v
	}
	return r
}
func (s *Service) Get(id TargetID) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Record{}, ErrUnavailable
	}
	r, ok := s.records[id]
	if !ok {
		r = Record{ID: id, View: View{State: Unknown}}
	}
	return clone(r), nil
}
func (s *Service) Snapshot() (map[TargetID]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, ErrUnavailable
	}
	m := map[TargetID]Record{}
	for id, r := range s.records {
		m[id] = clone(r)
	}
	return m, nil
}
func (s *Service) Warning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err != nil || len(s.dirty) > 0
}
func (s *Service) Observe(ctx context.Context, id TargetID, seq uint64, o Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] > 0 {
		s.active[id]--
	}
	if s.err != nil || id == "" {
		return ErrUnavailable
	}
	r := s.records[id]
	if seq <= r.AppliedSequence || (!o.Success && o.Kind == "") {
		return nil
	}
	now := s.now()
	r.ID = id
	r.AppliedSequence = seq
	r.UpdatedAt = now
	if o.Success {
		r.View = View{State: Healthy, LastSuccess: &now, LastFailure: r.LastFailure}
		r.Backoff = 0
		r.FailureTimes = nil
	} else {
		if r.State == "" {
			r.State = Unknown
		}
		r.LastFailure = &now
		r.Reason = string(o.Kind)
		r.Message = summary(o.Kind)
		r.Status = o.Status
		r.Code = o.Code
		recent := []time.Time{}
		for _, t := range r.FailureTimes {
			if !t.Before(now.Add(-s.policy.Window)) {
				recent = append(recent, t)
			}
		}
		recent = append(recent, now)
		if len(recent) > s.policy.Threshold {
			recent = recent[len(recent)-s.policy.Threshold:]
		}
		r.FailureTimes = recent
		if r.State == Isolated || o.Kind != Transient || len(recent) >= s.policy.Threshold {
			delay := s.policy.Delay(o.Kind)
			if r.State == Isolated && r.Backoff > 0 {
				delay = r.Backoff * 2
			}
			if delay > s.policy.MaxBackoff {
				delay = s.policy.MaxBackoff
			}
			r.Backoff = delay
			next := now.Add(s.jitter(delay))
			if o.RetryAt.After(next) {
				next = o.RetryAt
			}
			if r.NextProbe != nil && r.NextProbe.After(next) {
				next = *r.NextProbe
			}
			r.NextProbe = &next
			r.State = Isolated
		}
	}
	s.records[id] = r
	return s.persist(ctx, r)
}

// DeferProbe keeps an ignored probe result isolated without inventing a health failure.
func (s *Service) DeferProbe(ctx context.Context, id TargetID, seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok || r.State != Isolated || seq < r.AppliedSequence {
		return nil
	}
	delay := r.Backoff * 2
	if delay <= 0 {
		delay = s.policy.TransientDelay
	}
	if delay > s.policy.MaxBackoff {
		delay = s.policy.MaxBackoff
	}
	r.Backoff = delay
	t := s.now().Add(s.jitter(delay))
	if r.NextProbe != nil && r.NextProbe.After(t) {
		t = *r.NextProbe
	}
	r.NextProbe = &t
	r.UpdatedAt = s.now()
	s.records[id] = r
	return s.persist(ctx, r)
}
func summary(k Kind) string {
	switch k {
	case Permanent:
		return "Upstream credentials, quota or model configuration unavailable."
	case Limited:
		return "Upstream temporarily rate limited."
	default:
		return "Upstream connection or service temporarily unavailable."
	}
}

func (s *Service) jitter(d time.Duration) time.Duration {
	extra := time.Duration(rand.Float64() * float64(d) / 10)
	if d >= s.policy.MaxBackoff || extra > s.policy.MaxBackoff-d {
		return s.policy.MaxBackoff
	}
	return d + extra
}
func (s *Service) persist(ctx context.Context, r Record) error {
	e := s.store.SaveHealth(ctx, r)
	if e != nil {
		s.dirty[r.ID] = true
	} else {
		delete(s.dirty, r.ID)
	}
	return e
}

// Maintain retries failed writes and removes inactive obsolete generations only.
func (s *Service) Maintain(ctx context.Context, current map[TargetID]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	for id := range s.dirty {
		if ctx.Err() != nil {
			return
		}
		_ = s.persist(ctx, s.records[id])
	}
	remover, ok := s.store.(interface {
		DeleteHealth(context.Context, TargetID) error
	})
	if !ok {
		return
	}
	for id, r := range s.records {
		if ctx.Err() != nil {
			return
		}
		if !current[id] && s.active[id] == 0 && !s.dirty[id] && r.UpdatedAt.Before(s.now().Add(-7*24*time.Hour)) {
			if remover.DeleteHealth(ctx, id) == nil {
				delete(s.records, id)
				delete(s.active, id)
			}
		}
	}
}
