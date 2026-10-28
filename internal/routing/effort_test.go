package routing

import (
	"strings"
	"testing"
)

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
		got, err := ResolveAutoEffort(tc.allowed, tc.target)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	if _, err := ResolveAutoEffort([]string{"high"}, "bogus"); err == nil {
		t.Fatal("invalid decision accepted")
	}
}

func TestAutomaticEffortLevels(t *testing.T) {
	m := Model{Capabilities: Capabilities{Reasoning: []ReasoningCombination{
		{Effort: "high"}, {Effort: "low"}, {Effort: "low"}, {Effort: "none"}, {Effort: "minimal"}, {Effort: "xhigh"}, {Effort: "max", EnableThinking: new(false)}, {EnableThinking: new(true)},
	}}}
	for _, tc := range []struct {
		protocol ProviderProtocol
		want     string
	}{{ProtocolOpenAI, "minimal,low,high,xhigh"}, {ProtocolAnthropic, "low,high"}} {
		if got := strings.Join(AutomaticEffortLevels(m, tc.protocol), ","); got != tc.want {
			t.Fatalf("%s got %s", tc.protocol, got)
		}
	}
}
