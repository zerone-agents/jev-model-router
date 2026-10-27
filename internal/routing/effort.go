package routing

import "encoding/json"

// AutoEffortConfig declares operator-approved upstream effort levels.
// It does not assert comparable capability across models.
type AutoEffortConfig struct {
	Allowed []string `json:"allowed"`
}

var effortLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

func effortRank(s string) int {
	for i, v := range effortLevels {
		if s == v {
			return i
		}
	}
	return -1
}

func ResolveAutoEffort(config *AutoEffortConfig, requested string) (string, error) {
	if config == nil {
		return "", nil
	}
	rank := effortRank(requested)
	if rank < 0 {
		return "", Fail("invalid_decision", "invalid automatic effort")
	}
	lowest, best := len(effortLevels), -1
	for _, v := range config.Allowed {
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

func AutomaticEffortAllowed(r Request) bool {
	if r.Model != "auto" || r.AutoEffortBlocked || r.Options["reasoning_effort"] != nil {
		return false
	}
	var knobs struct {
		Enable *bool `json:"enable_thinking"`
	}
	_ = json.Unmarshal(r.Options["chat_template_kwargs"], &knobs)
	return knobs.Enable == nil || *knobs.Enable
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
