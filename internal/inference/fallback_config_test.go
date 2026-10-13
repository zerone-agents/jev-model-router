package inference

import (
	"testing"
	"time"
)

func TestFallbackConfig(t *testing.T) {
	c := DefaultFallbackConfig()
	if !c.Enabled || c.MaxRetries != 1 || c.Timeout != 180*time.Second {
		t.Fatal(c)
	}
	for _, n := range []int{-1, 2} {
		c.MaxRetries = n
		if c.Validate() == nil {
			t.Fatal(n)
		}
	}
	c = DefaultFallbackConfig()
	c.Timeout = 31 * time.Minute
	if c.Validate() == nil {
		t.Fatal("timeout accepted")
	}
}
