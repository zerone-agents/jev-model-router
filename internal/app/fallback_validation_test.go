package app

import "testing"

func TestFallbackTimeoutConfigRange(t *testing.T) {
	for _, v := range []string{"1ms", "31m"} {
		t.Run(v, func(t *testing.T) {
			c, e := LoadConfig("", func(k string) (string, bool) {
				if k == "JEV_ROUTER_AUTO_FALLBACK_TIMEOUT" {
					return v, true
				}
				return "", false
			})
			if e == nil {
				t.Fatalf("invalid fallback timeout accepted: %v (Fallback.Validate=%v)", c.Fallback.Timeout, c.Fallback.Validate())
			}
		})
	}
}

func TestFallbackTimeoutConfigBoundaries(t *testing.T) {
	for _, v := range []string{"1s", "180s", "30m"} {
		t.Run(v, func(t *testing.T) {
			_, err := LoadConfig("", func(k string) (string, bool) { return v, k == "JEV_ROUTER_AUTO_FALLBACK_TIMEOUT" })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
