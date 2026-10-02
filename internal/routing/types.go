package routing

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/contracts"
)

type Provider struct {
	ID        string `json:"id"`
	BaseURL   string `json:"base_url"`
	SecretRef string `json:"secret_ref"`
}

// ReasoningCombination declares supported values for independent controls.
// Values are collected across all entries; nil declares no thinking value.
type ReasoningCombination struct {
	EnableThinking *bool  `json:"enable_thinking,omitempty"`
	Effort         string `json:"reasoning_effort,omitempty"`
}

type Capabilities struct {
	Reasoning        []ReasoningCombination `json:"reasoning,omitempty"`
	ContextLimit     int64                  `json:"context_limit"`
	Tools            bool                   `json:"tools"`
	Images           bool                   `json:"images"`
	StructuredOutput bool                   `json:"structured_output"`
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
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	Refusal          *string         `json:"refusal,omitempty"`
	Role             string          `json:"role,omitempty"`
	Content          json.RawMessage `json:"content,omitempty"`
	Name             string          `json:"name,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
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
			return Fail("invalid_request", "invalid chat field: "+k)
		}
	}
	// Retain raw input for validation, but expose one output limit internally.
	if value, ok := r.Options["max_tokens"]; ok && r.Options["max_completion_tokens"] == nil {
		r.Options["max_completion_tokens"] = value
		delete(r.Options, "max_tokens")
	}
	// JSON Schema integers also include 1024.0 and 1.024e3. Canonicalize their
	// numeric representation for the adapter's integer decoder without rounding.
	if value := r.Options["max_completion_tokens"]; value != nil && contracts.ValidChatOutputLimit(value) {
		var number float64
		if err := json.Unmarshal(value, &number); err != nil {
			return err
		}
		r.Options["max_completion_tokens"], _ = json.Marshal(int64(number))
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
	TotalTokens   int64           `json:"total_tokens,omitempty"`
	InputDetails  json.RawMessage `json:"input_details,omitempty"`
	OutputDetails json.RawMessage `json:"output_details,omitempty"`
	InputTokens   int64           `json:"input_tokens"`
	OutputTokens  int64           `json:"output_tokens"`
}
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Fail(code, message string) error { return &Error{Code: code, Message: message} }

type Target struct {
	Provider Provider `json:"-"`
	Model    Model    `json:"-"`
}
type Completion struct {
	ID      string   `json:"id"`
	Created int64    `json:"created"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"-"`
}
type Event = Completion
type Delta = Message
type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}
type EventStream interface {
	Next(context.Context) (Event, error)
	Close() error
}
type Generator interface {
	Complete(context.Context, Target, Request) (Completion, error)
	Stream(context.Context, Target, Request) (EventStream, error)
}

type DecisionInput struct {
	Prompt     string
	Candidates []Model
	Request    Request
}
type Decision struct {
	ModelID string
	Usage   *Usage
}
type Decider interface {
	Choose(context.Context, DecisionConfig, DecisionInput) (Decision, error)
}
