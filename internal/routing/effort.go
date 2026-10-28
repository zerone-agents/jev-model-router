package routing

import "encoding/json"

var effortLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

func effortRank(s string) int {
	for i, v := range effortLevels {
		if s == v {
			return i
		}
	}
	return -1
}

func ResolveAutoEffort(allowed []string, requested string) (string, error) {
	if len(allowed) == 0 {
		return "", nil
	}
	rank := effortRank(requested)
	if rank < 0 {
		return "", Fail("invalid_decision", "invalid automatic effort")
	}
	lowest, best := len(effortLevels), -1
	for _, v := range allowed {
		n := effortRank(v)
		if n < 0 {
			return "", Fail("invalid_request", "invalid automatic effort configuration")
		}
		if n < lowest {
			lowest = n
		}
		if n <= rank && n > best {
			best = n
		}
	}
	if lowest == len(effortLevels) {
		return "", Fail("invalid_request", "empty automatic effort configuration")
	}
	if best < 0 {
		best = lowest
	}
	return effortLevels[best], nil
}

// AutomaticEffortLevels is the single source for planning and read-only metadata.
// Provider adaptation filters unsupported levels without rewriting stored declarations.
func AutomaticEffortLevels(m Model, protocol ProviderProtocol) []string {
	levels := []string{}
	for _, level := range effortLevels {
		if protocol == ProtocolAnthropic && (level == "minimal" || level == "xhigh") {
			continue
		}
		for _, entry := range m.Capabilities.Reasoning {
			if entry.Effort == level && (entry.EnableThinking == nil || *entry.EnableThinking) {
				levels = append(levels, level)
				break
			}
		}
	}
	return levels
}

// ValidateAutoEffortControls runs on original requests, never injected copies.
func ValidateAutoEffortControls(r Request) error {
	if r.Model != "auto" {
		return nil
	}
	var knobs struct {
		Enable *bool `json:"enable_thinking"`
	}
	_ = json.Unmarshal(r.Options["chat_template_kwargs"], &knobs)
	if r.AutoEffortBlocked || r.Options["reasoning_effort"] != nil || (knobs.Enable != nil && !*knobs.Enable) {
		return Fail("invalid_request", "Auto selects reasoning effort; use an explicit model for manual effort, disabled thinking or a fixed thinking budget")
	}
	return nil
}
func ExplicitEffort(r Request) string {
	var effort string
	_ = json.Unmarshal(r.Options["reasoning_effort"], &effort)
	return effort
}

func WithEffectiveEffort(r Request, effort string) Request {
	if effort == "" {
		return r
	}
	options := make(map[string]json.RawMessage, len(r.Options)+1)
	for k, v := range r.Options {
		options[k] = append(json.RawMessage(nil), v...)
	}
	options["reasoning_effort"], _ = json.Marshal(effort)
	r.Options = options
	r.raw = nil
	return r
}

// Anthropic represents Chat's explicit none as thinking.disabled, not effort.
func outgoingEffort(target Target, effort string) string {
	if target.Provider.Protocol == ProtocolAnthropic && effort == "none" {
		return ""
	}
	return effort
}
