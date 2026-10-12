package health

import (
	"errors"
	"time"
)

type Config struct {
	Policy                 Policy
	Concurrency, MaxTokens int
	Interval, Timeout      time.Duration
}

func DefaultConfig() Config {
	return Config{Policy: DefaultPolicy(), Concurrency: 2, MaxTokens: 1024, Interval: 5 * time.Second, Timeout: time.Minute}
}
func (c *Config) Numbers() map[string]*int {
	return map[string]*int{"concurrency": &c.Concurrency, "max_tokens": &c.MaxTokens, "failure_threshold": &c.Policy.Threshold}
}
func (c *Config) Durations() map[string]*time.Duration {
	return map[string]*time.Duration{"interval": &c.Interval, "timeout": &c.Timeout, "failure_window": &c.Policy.Window, "transient_delay": &c.Policy.TransientDelay, "limit_delay": &c.Policy.LimitDelay, "permanent_delay": &c.Policy.PermanentDelay, "max_backoff": &c.Policy.MaxBackoff}
}
func (c Config) Validate() error {
	if c.Concurrency < 1 || c.Concurrency > 16 || c.MaxTokens < 16 || c.MaxTokens > 65536 || c.Policy.Threshold < 1 || c.Policy.Threshold > 100 {
		return errors.New("invalid health numeric limit")
	}
	for _, d := range (&c).Durations() {
		if *d < time.Second || *d > 7*24*time.Hour {
			return errors.New("invalid health duration")
		}
	}
	if c.Policy.MaxBackoff < c.Policy.TransientDelay || c.Policy.MaxBackoff < c.Policy.LimitDelay || c.Policy.MaxBackoff < c.Policy.PermanentDelay {
		return errors.New("health max_backoff below initial delay")
	}
	return nil
}
