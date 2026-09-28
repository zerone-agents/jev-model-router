package management

import (
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type Principal struct {
	ID   string
	Role string
}
type Call struct {
	CapabilityID    string          `json:"capability_id,omitempty"`
	Input           json.RawMessage `json:"input"`
	ExpectedVersion *int64          `json:"expected_version,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
}
type Error = routing.Error
type Meta struct {
	OperationID   string `json:"operation_id"`
	Timestamp     string `json:"timestamp"`
	SchemaVersion string `json:"schema_version"`
}
type Result struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    *Error          `json:"error,omitempty"`
	Warnings []string        `json:"warnings"`
	Meta     Meta            `json:"meta"`
}
