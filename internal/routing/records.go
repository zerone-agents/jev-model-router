package routing

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

type Record struct {
	RequestID      string    `json:"request_id"`
	ConfigVersion  int64     `json:"config_version,omitempty"`
	Mode           string    `json:"mode,omitempty"`
	Path           string    `json:"path,omitempty"`
	CandidateIDs   []string  `json:"candidate_ids,omitempty"`
	ModelID        string    `json:"model_id,omitempty"`
	DecisionMillis int64     `json:"decision_ms"`
	RequestSummary string    `json:"request_summary,omitempty"`
	Outcome        string    `json:"outcome"`
	ErrorCode      string    `json:"error_code,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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

// RequestSummary retains only bounded text from the last user message. Tool
// payloads, image URLs and earlier conversation messages are never included.
func RequestSummary(req Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		var texts []string
		var plain string
		if json.Unmarshal(m.Content, &plain) == nil {
			texts = []string{plain}
		} else {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(m.Content, &parts) != nil {
				return ""
			}
			for _, part := range parts {
				if part.Type == "text" {
					texts = append(texts, part.Text)
				}
			}
		}
		out := make([]rune, 0, 120)
		space := false
		for _, text := range texts {
			for _, r := range text {
				if unicode.IsSpace(r) {
					space = len(out) > 0
					continue
				}
				if unicode.IsControl(r) {
					continue
				}
				if space {
					if len(out) == 120 {
						return string(out)
					}
					out = append(out, ' ')
					space = false
				}
				if len(out) == 120 {
					return strings.TrimSpace(string(out))
				}
				out = append(out, r)
			}
			space = len(out) > 0
		}
		return string(out)
	}
	return ""
}
