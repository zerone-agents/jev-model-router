// Package health tracks generation target availability independently of configuration.
package health

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrUnavailable = errors.New("health storage unavailable")

type TargetID string
type State string

const (
	Unknown  State = "unknown"
	Healthy  State = "healthy"
	Isolated State = "isolated"
)

type View struct {
	State       State      `json:"state"`
	LastSuccess *time.Time `json:"last_success_at"`
	LastFailure *time.Time `json:"last_failure_at"`
	Reason      string     `json:"reason,omitempty"`
	Message     string     `json:"message,omitempty"`
	Status      int        `json:"upstream_status,omitempty"`
	Code        string     `json:"upstream_code,omitempty"`
	NextProbe   *time.Time `json:"next_probe_at"`
}
type Record struct {
	View
	ID              TargetID      `json:"id"`
	FailureTimes    []time.Time   `json:"failure_times,omitempty"`
	Backoff         time.Duration `json:"backoff"`
	AppliedSequence uint64        `json:"sequence"`
	UpdatedAt       time.Time     `json:"updated_at"`
}
type Store interface {
	LoadHealthKey(context.Context) ([32]byte, error)
	LoadHealth(context.Context) ([]Record, error)
	SaveHealth(context.Context, Record) error
}

func Fingerprint(key [32]byte, value []byte) string {
	h := hmac.New(sha256.New, key[:])
	h.Write([]byte("jev-router/credential/v1\x00"))
	h.Write(value)
	return hex.EncodeToString(h.Sum(nil))
}
func Identity(key [32]byte, parts ...string) TargetID {
	b, _ := json.Marshal(parts)
	h := hmac.New(sha256.New, key[:])
	h.Write([]byte("jev-router/health-target/v1\x00"))
	h.Write(b)
	return TargetID(hex.EncodeToString(h.Sum(nil)))
}
