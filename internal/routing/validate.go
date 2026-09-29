package routing

import (
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/contracts"
	"net/url"
	"strings"
)

func ValidateSnapshot(s Snapshot) error {
	providers := map[string]bool{}
	models := map[string]bool{}
	for _, p := range s.Providers {
		b, _ := json.Marshal(p)
		if contracts.Validate("providers.put", b) != nil || providers[p.ID] || !validURL(p.BaseURL) {
			return Fail("invalid_request", "invalid or duplicate provider")
		}
		providers[p.ID] = true
	}
	for _, m := range s.Models {
		b, _ := json.Marshal(m)
		if contracts.Validate("models.put", b) != nil || models[m.ID] || !providers[m.ProviderID] {
			return Fail("invalid_request", "invalid model, reserved ID, or missing provider")
		}
		models[m.ID] = true
	}
	if s.Decision != (DecisionConfig{}) {
		b, _ := json.Marshal(s.Decision)
		if contracts.Validate("decision.put", b) != nil || !validURL(s.Decision.BaseURL) {
			return Fail("invalid_request", "invalid decision configuration")
		}
	}
	prompt, _ := json.Marshal(map[string]string{"text": s.Prompt})
	if contracts.Validate("prompt.put", prompt) != nil {
		return Fail("invalid_request", "invalid routing prompt")
	}
	return nil
}
func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func ValidateRequest(r Request) error {
	b := r.raw
	if b == nil {
		b, _ = json.Marshal(r)
	}
	if contracts.Validate("chat", b) != nil {
		return Fail("unsupported_request", "request is outside the supported chat schema")
	}
	if len(r.Tools) == 0 && (r.Options["tool_choice"] != nil || r.Options["parallel_tool_calls"] != nil) {
		return Fail("invalid_request", "tool options require tools")
	}
	if !r.Stream && r.Options["stream_options"] != nil {
		return Fail("invalid_request", "stream_options requires streaming")
	}
	pending := map[string]bool{}
	for _, m := range r.Messages {
		if len(pending) > 0 && m.Role != "tool" {
			return Fail("invalid_request", "tool results must follow their calls")
		}
		if m.Role == "tool" {
			if !pending[m.ToolCallID] {
				return Fail("invalid_request", "orphan tool result")
			}
			delete(pending, m.ToolCallID)
		} else if m.ToolCallID != "" {
			return Fail("invalid_request", "tool_call_id only belongs to tool results")
		}
		if len(m.ToolCalls) > 0 {
			if m.Role != "assistant" {
				return Fail("invalid_request", "tool calls require assistant role")
			}
			for _, c := range m.ToolCalls {
				if c.ID == "" || pending[c.ID] {
					return Fail("invalid_request", "duplicate or missing tool call ID")
				}
				pending[c.ID] = true
			}
		}
		if len(m.Content) == 0 && len(m.ToolCalls) == 0 {
			return Fail("invalid_request", "message has no content")
		}
	}
	if len(pending) > 0 {
		return Fail("invalid_request", "missing tool results")
	}
	return nil
}
func HasImages(r Request) bool {
	for _, m := range r.Messages {
		if strings.HasPrefix(strings.TrimSpace(string(m.Content)), "[") {
			var parts []struct {
				Type string `json:"type"`
			}
			json.Unmarshal(m.Content, &parts)
			for _, p := range parts {
				if p.Type == "image_url" {
					return true
				}
			}
		}
	}
	return false
}

// RequiresStructuredOutput separates plain text from JSON output modes.
func RequiresStructuredOutput(r Request) bool {
	var format struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(r.Options["response_format"], &format) != nil {
		return false
	}
	return format.Type == "json_object" || format.Type == "json_schema"
}
