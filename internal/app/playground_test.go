package app

import (
	"testing"
	"time"
)

func TestPlaygroundDefaultsAndOverrides(t *testing.T) {
	c, e := LoadConfig("", func(string) (string, bool) { return "", false })
	if e != nil {
		t.Fatal(e)
	}
	p := c.Playground
	if !p.Enabled || p.SessionRPM != 6 || p.InstanceRPM != 20 || p.SessionConcurrency != 1 || p.InstanceConcurrency != 3 || p.DailyRequests != 200 || p.InputBytes != 32768 || p.BodyBytes != 262144 || p.MaxMessages != 100 || p.OutputTokens != 4096 || p.Timeout != 120*time.Second {
		t.Fatalf("defaults: %+v", p)
	}
	c, e = LoadConfig("", func(k string) (string, bool) {
		v, ok := map[string]string{"JEV_ROUTER_PLAYGROUND_ENABLED": "false", "JEV_ROUTER_PLAYGROUND_SESSION_RPM": "8"}[k]
		return v, ok
	})
	if e != nil || c.Playground.Enabled || c.Playground.SessionRPM != 8 {
		t.Fatalf("override: %+v %v", c.Playground, e)
	}
	for _, v := range []string{"0", "-1", "1000001", "oops"} {
		_, e = LoadConfig("", func(k string) (string, bool) { return v, k == "JEV_ROUTER_PLAYGROUND_SESSION_RPM" })
		if e == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}
