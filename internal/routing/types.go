package routing

import (
	"encoding/json"
	"fmt"
)

type Provider struct {
	ID        string `json:"id"`
	BaseURL   string `json:"base_url"`
	SecretRef string `json:"secret_ref"`
}
type Capabilities struct {
	ContextLimit     int64 `json:"context_limit"`
	Tools            bool  `json:"tools"`
	Images           bool  `json:"images"`
	StructuredOutput bool  `json:"structured_output"`
}
type Model struct {
	ID           string       `json:"id"`
	ProviderID   string       `json:"provider_id"`
	UpstreamName string       `json:"upstream_name"`
	Description  string       `json:"description"`
	Location     string       `json:"location"`
	Enabled      bool         `json:"enabled"`
	Capabilities Capabilities `json:"capabilities"`
}
type DecisionConfig struct {
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	SecretRef string `json:"secret_ref"`
}
type Snapshot struct {
	Version   int64          `json:"version"`
	Providers []Provider     `json:"providers"`
	Models    []Model        `json:"models"`
	Decision  DecisionConfig `json:"decision"`
	Prompt    string         `json:"prompt"`
}
type Message struct {
	Role       string          `json:"role,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
}
type Function struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function CallFunction `json:"function"`
}
type CallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}
type Request struct {
	Model    string
	Messages []Message
	Tools    []Tool
	Options  map[string]json.RawMessage
	Stream   bool
	raw      json.RawMessage
}

func (r *Request) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if e := json.Unmarshal(b, &m); e != nil {
		return e
	}
	r.Options = map[string]json.RawMessage{}
	r.raw = append(r.raw[:0], b...)
	for k, v := range m {
		var e error
		switch k {
		case "model":
			e = json.Unmarshal(v, &r.Model)
		case "messages":
			e = json.Unmarshal(v, &r.Messages)
		case "tools":
			e = json.Unmarshal(v, &r.Tools)
		case "stream":
			e = json.Unmarshal(v, &r.Stream)
		default:
			r.Options[k] = v
		}
		if e != nil {
			return fmt.Errorf("invalid %s", k)
		}
	}
	return nil
}
func (r Request) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	for k, v := range r.Options {
		m[k] = v
	}
	m["model"] = r.Model
	m["messages"] = r.Messages
	if len(r.Tools) > 0 {
		m["tools"] = r.Tools
	}
	if r.Stream {
		m["stream"] = true
	}
	return json.Marshal(m)
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Fail(code, message string) error { return &Error{Code: code, Message: message} }
