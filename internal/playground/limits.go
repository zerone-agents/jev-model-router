// Package playground owns bounded admission for browser-only generation.
package playground

import (
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"time"
)

type Limits struct {
	Enabled             bool          `json:"enabled"`
	SessionRPM          int           `json:"session_rpm"`
	InstanceRPM         int           `json:"instance_rpm"`
	SessionConcurrency  int           `json:"session_concurrency"`
	InstanceConcurrency int           `json:"instance_concurrency"`
	DailyRequests       int           `json:"daily_requests"`
	InputBytes          int           `json:"input_bytes"`
	BodyBytes           int           `json:"body_bytes"`
	MaxMessages         int           `json:"max_messages"`
	OutputTokens        int           `json:"output_tokens"`
	Timeout             time.Duration `json:"-"`
}

func (l *Limits) Numbers() map[string]*int {
	return map[string]*int{"session_rpm": &l.SessionRPM, "instance_rpm": &l.InstanceRPM, "session_concurrency": &l.SessionConcurrency, "instance_concurrency": &l.InstanceConcurrency, "daily_requests": &l.DailyRequests, "input_bytes": &l.InputBytes, "body_bytes": &l.BodyBytes, "max_messages": &l.MaxMessages, "output_tokens": &l.OutputTokens}
}

type bound struct {
	Default int
	Minimum int
	Maximum int
}

func bounds() map[string]bound {
	var p struct{ Limits map[string]bound }
	if err := json.Unmarshal(contracts.PlaygroundPolicy(), &p); err != nil {
		panic(err)
	}
	return p.Limits
}
func DefaultLimits() Limits {
	l := Limits{Enabled: true}
	b := bounds()
	for k, p := range l.Numbers() {
		*p = b[k].Default
	}
	l.Timeout = time.Duration(b["timeout_seconds"].Default) * time.Second
	return l
}
func (l Limits) Validate() error {
	b := bounds()
	for k, p := range l.Numbers() {
		if *p < b[k].Minimum || *p > b[k].Maximum {
			return errors.New("invalid playground limit: " + k)
		}
	}
	if l.InputBytes > l.BodyBytes || l.Timeout < time.Duration(b["timeout_seconds"].Minimum)*time.Second || l.Timeout > time.Duration(b["timeout_seconds"].Maximum)*time.Second {
		return errors.New("invalid playground input/body or timeout limit")
	}
	return nil
}
