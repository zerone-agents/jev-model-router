package routing

import "testing"

func TestResolveAutoEffort(t *testing.T) {
	for _, tc := range []struct {
		allowed      []string
		target, want string
	}{
		{[]string{"high", "low", "medium"}, "xhigh", "high"},
		{[]string{"high", "medium"}, "minimal", "medium"},
		{[]string{"high"}, "max", "high"},
		{nil, "xhigh", ""},
	} {
		var c *AutoEffortConfig
		if tc.allowed != nil {
			c = &AutoEffortConfig{Allowed: tc.allowed}
		}
		got, err := ResolveAutoEffort(c, tc.target)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	if _, err := ResolveAutoEffort(&AutoEffortConfig{Allowed: []string{"high"}}, "bogus"); err == nil {
		t.Fatal("invalid decision accepted")
	}
}
