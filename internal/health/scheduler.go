package health

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrProbeBusy = errors.New("health probe already in progress or capacity reached")

type ProbeLimiter struct {
	mu     sync.Mutex
	active map[TargetID]bool
	limit  int
}

func NewProbeLimiter(limit int) *ProbeLimiter {
	return &ProbeLimiter{active: map[TargetID]bool{}, limit: limit}
}
func (p *ProbeLimiter) Acquire(id TargetID) (func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active[id] || len(p.active) >= p.limit {
		return nil, ErrProbeBusy
	}
	p.active[id] = true
	var once sync.Once
	return func() { once.Do(func() { p.mu.Lock(); defer p.mu.Unlock(); delete(p.active, id) }) }, nil
}

// Work must be bounded by the caller's probe limiter. No backlog is accumulated.
func RunScheduler(ctx context.Context, interval time.Duration, work func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() == nil {
				work(ctx)
			}
		}
	}
}
func (p *ProbeLimiter) Active(id TargetID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active[id] || len(p.active) >= p.limit
}
