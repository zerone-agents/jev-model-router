package routing

import "encoding/json"

// CheckReasoning matches complete combinations, never the Cartesian product of
// separately supported controls. No controls retains the upstream default.
func CheckReasoning(c Capabilities, r Request) error {
	var requested ReasoningCombination
	if raw := r.Options["chat_template_kwargs"]; raw != nil {
		if json.Unmarshal(raw, &requested) != nil || requested.EnableThinking == nil {
			return Fail("unsupported_request", "chat_template_kwargs requires boolean enable_thinking")
		}
	}
	if raw := r.Options["reasoning_effort"]; raw != nil {
		if json.Unmarshal(raw, &requested.Effort) != nil || requested.Effort == "" {
			return Fail("unsupported_request", "invalid reasoning_effort")
		}
	}
	if requested.EnableThinking == nil && requested.Effort == "" {
		return nil
	}
	for _, supported := range c.Reasoning {
		if supported.Effort != requested.Effort {
			continue
		}
		if supported.EnableThinking == nil && requested.EnableThinking == nil {
			return nil
		}
		if supported.EnableThinking != nil && requested.EnableThinking != nil && *supported.EnableThinking == *requested.EnableThinking {
			return nil
		}
	}
	return Fail("unsupported_request", "model does not support the requested reasoning control combination; configure capabilities.reasoning with a verified combination or select a compatible model")
}
