package inference

import (
	"errors"
	"time"
)

type FallbackConfig struct {
	Enabled    bool          `json:"enabled"`
	MaxRetries int           `json:"max_retries"`
	Timeout    time.Duration `json:"-"`
}

func DefaultFallbackConfig() FallbackConfig { return FallbackConfig{true, 1, 180 * time.Second} }
func (c FallbackConfig) Validate() error {
	if c.MaxRetries < 0 || c.MaxRetries > 1 || c.Timeout < time.Second || c.Timeout > 30*time.Minute {
		return errors.New("invalid auto fallback configuration")
	}
	return nil
}
