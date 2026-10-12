package health

import (
	"net/http"
	"strconv"
	"time"
)

type Kind string

const (
	Transient Kind = "temporary_failure"
	Permanent Kind = "configuration_or_quota"
	Limited   Kind = "rate_limited"
)

type Outcome struct {
	Success bool
	Kind    Kind
	Status  int
	Code    string
	RetryAt time.Time
}
type Policy struct {
	Window                                                 time.Duration
	Threshold                                              int
	TransientDelay, LimitDelay, PermanentDelay, MaxBackoff time.Duration
}

func DefaultPolicy() Policy {
	return Policy{2 * time.Minute, 3, 30 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute}
}
func ParseRetryAfter(v string, now time.Time) time.Time {
	if n, e := strconv.ParseUint(v, 10, 64); e == nil {
		if n > 0 && n <= uint64((1<<63-1)/int64(time.Second)) {
			return now.Add(time.Duration(n) * time.Second)
		}
		return time.Time{}
	}
	if t, e := http.ParseTime(v); e == nil && t.After(now) {
		return t
	}
	return time.Time{}
}
func (p Policy) Delay(k Kind) time.Duration {
	switch k {
	case Permanent:
		return p.PermanentDelay
	case Limited:
		return p.LimitDelay
	default:
		return p.TransientDelay
	}
}
