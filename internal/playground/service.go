package playground

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Quota struct {
	SessionRemaining  int       `json:"session_remaining"`
	InstanceRemaining int       `json:"instance_remaining"`
	DayRemaining      int       `json:"day_remaining"`
	ResetAt           time.Time `json:"reset_at"`
}
type LimitError struct {
	Scope      string
	RetryAfter time.Duration
	ResetAt    *time.Time
}

func (e *LimitError) Error() string { return "playground request limit reached" }

type Store interface {
	CheckPlayground(context.Context, string, time.Time, Limits) error
	AdmitPlayground(context.Context, string, time.Time, Limits) (Quota, error)
	PlaygroundQuota(context.Context, string, time.Time, Limits) (Quota, error)
}
type Service struct {
	store    Store
	limits   Limits
	now      func() time.Time
	mu       sync.Mutex
	active   int
	sessions map[string]int
}

func NewService(store Store, limits Limits, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, limits: limits, now: now, sessions: map[string]int{}}
}

type Lease struct {
	once    sync.Once
	release func()
}

func (l *Lease) Release() { l.once.Do(l.release) }
func (s *Service) Acquire(ctx context.Context, id string) (*Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope := ""
	if s.sessions[id] >= s.limits.SessionConcurrency {
		scope = "session_concurrency"
	}
	if s.active >= s.limits.InstanceConcurrency {
		scope = "instance_concurrency"
	}
	if scope != "" {
		// A concurrency rejection must not consume quota, but a longer durable
		// limit (or unavailable storage) still determines the response.
		err := s.store.CheckPlayground(ctx, id, s.now().UTC(), s.limits)
		var limit *LimitError
		if err != nil && (!errors.As(err, &limit) || limit.RetryAfter >= time.Second) {
			return nil, err
		}
		return nil, &LimitError{Scope: scope, RetryAfter: time.Second}
	}
	if _, err := s.store.AdmitPlayground(ctx, id, s.now().UTC(), s.limits); err != nil {
		return nil, err
	}
	s.active++
	s.sessions[id]++
	return &Lease{release: func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active--
		s.sessions[id]--
		if s.sessions[id] == 0 {
			delete(s.sessions, id)
		}
	}}, nil
}
func (s *Service) Status(ctx context.Context, id string) (Quota, error) {
	return s.store.PlaygroundQuota(ctx, id, s.now().UTC(), s.limits)
}
