package routing

import (
	"context"
	"sync/atomic"
	"time"
)

type Record struct {
	RequestID        string    `json:"request_id"`
	ConfigVersion    int64     `json:"config_version,omitempty"`
	Mode             string    `json:"mode,omitempty"`
	Path             string    `json:"path,omitempty"`
	CandidateIDs     []string  `json:"candidate_ids,omitempty"`
	ModelID          string    `json:"model_id,omitempty"`
	DecisionMillis   int64     `json:"decision_ms"`
	GenerationMillis int64     `json:"generation_ms"`
	Outcome          string    `json:"outcome"`
	ErrorCode        string    `json:"error_code,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
type RecordSink interface {
	Append(context.Context, Record) error
}
type Recorder struct {
	Sink     RecordSink
	Timeout  time.Duration
	degraded atomic.Bool
}

func (r *Recorder) Degraded() bool { return r.degraded.Load() }
func (r *Recorder) MarkDegraded()  { r.degraded.Store(true) }

// Save is synchronous and bounded by the sink's context-aware storage operation.
// Failure is sticky until restart, ensuring intermittent loss remains visible.
func (r *Recorder) Save(record Record) {
	if r == nil || r.Sink == nil {
		return
	}
	d := r.Timeout
	if d <= 0 {
		d = 100 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if r.Sink.Append(ctx, record) != nil {
		r.MarkDegraded()
	}
}
