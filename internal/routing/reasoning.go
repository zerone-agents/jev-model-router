package routing

import "encoding/json"

// CheckReasoning validates each supplied control independently.
// Omitted controls retain the upstream default.
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
	thinkingSupported := requested.EnableThinking == nil
	effortSupported := requested.Effort == ""
	for _, supported := range c.Reasoning {
		if requested.EnableThinking != nil && supported.EnableThinking != nil && *supported.EnableThinking == *requested.EnableThinking {
			thinkingSupported = true
		}
		if requested.Effort != "" && supported.Effort == requested.Effort {
			effortSupported = true
		}
	}
	if !thinkingSupported {
		return Fail("unsupported_request", "model does not support the requested enable_thinking value; configure capabilities.reasoning or select a compatible model")
	}
	if !effortSupported {
		return Fail("unsupported_request", "model does not support the requested reasoning_effort value; configure capabilities.reasoning or select a compatible model")
	}
	return nil
}
