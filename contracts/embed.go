package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed management.json schemas/*.json
var files embed.FS

type CallContract struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Schema   json.RawMessage `json:"schema"`
	Protocol json.RawMessage `json:"protocol"`
}
type Capability struct {
	Call         CallContract      `json:"call"`
	ID           string            `json:"id"`
	Description  string            `json:"description"`
	Role         string            `json:"role"`
	InputSchema  json.RawMessage   `json:"input_schema"`
	OutputSchema json.RawMessage   `json:"output_schema"`
	SideEffects  []string          `json:"side_effects"`
	Risk         string            `json:"risk"`
	Availability string            `json:"availability"`
	Examples     []json.RawMessage `json:"examples"`
	Write        bool              `json:"write"`
}

var once sync.Once
var caps []Capability
var validators map[string]*jsonschema.Schema
var idempotencyHours int

func initSchemas() {
	b, e := files.ReadFile("management.json")
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(b, &caps); e != nil {
		panic(e)
	}
	rules, err := files.ReadFile("schemas/results.json")
	if err != nil {
		panic(err)
	}
	var protocol map[string]any
	if err = json.Unmarshal(rules, &protocol); err != nil {
		panic(err)
	}
	idempotencyHours = int(protocol["retention"].(map[string]any)["idempotency_hours"].(float64))
	protocol["idempotency"].(map[string]any)["valid_for_hours"] = idempotencyHours
	protocol["session"] = SessionPolicy()
	encodedProtocol, _ := json.Marshal(protocol)
	for i := range caps {
		schema := callSchema(caps[i].Write)
		props := schema["properties"].(map[string]any)
		props["input"] = caps[i].InputSchema
		props["capability_id"] = map[string]any{"const": caps[i].ID}
		body, _ := json.Marshal(schema)
		caps[i].Call = CallContract{Method: "POST", Path: "/admin/v1/call/" + caps[i].ID, Schema: body, Protocol: encodedProtocol}
	}
	validators = map[string]*jsonschema.Schema{}
	envelope, _ := json.Marshal(callSchema(true))
	validators["write_envelope"] = compile(envelope)
	policy := SessionLimits()
	validators["session.login"] = compile(policy.InputSchema)
	validators["session.logout"] = compile(policy.InputSchema)
	for _, c := range caps {
		validators[c.ID] = compile(c.InputSchema)
	}
	b, _ = files.ReadFile("schemas/chat.json")
	validators["chat"] = compile(b)
}
func compile(b []byte) *jsonschema.Schema {
	var v any
	if e := json.Unmarshal(b, &v); e != nil {
		panic(e)
	}
	c := jsonschema.NewCompiler()
	if e := c.AddResource("https://router.invalid/schema", v); e != nil {
		panic(e)
	}
	s, e := c.Compile("https://router.invalid/schema")
	if e != nil {
		panic(e)
	}
	return s
}
func Catalog() []Capability {
	once.Do(initSchemas)
	b, _ := json.Marshal(caps)
	var out []Capability
	json.Unmarshal(b, &out)
	return out
}
func Validate(id string, input json.RawMessage) error {
	once.Do(initSchemas)
	s, ok := validators[id]
	if !ok {
		return errors.New("unknown capability")
	}
	v, e := Decode(input)
	if e != nil {
		return e
	}
	if id == "providers.put" {
		if !utf8.Valid(input) {
			return errors.New("invalid UTF-8 input")
		}
		if obj, ok := v.(map[string]any); ok {
			if key, ok := obj["api_key"].(string); ok && len(key) > 16384 {
				return errors.New("credential exceeds 16384 UTF-8 bytes")
			}
		}
	}
	if e = s.Validate(v); e != nil {
		return fmt.Errorf("input does not match %s schema", id)
	}
	return nil
}
func Decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if e := d.Decode(&v); e != nil {
		return nil, errors.New("invalid JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("multiple JSON values")
	}
	return v, nil
}

type Mapping struct {
	HTTP int `json:"http_status"`
	Exit int `json:"exit_code"`
}

func ErrorMapping(code string) Mapping {
	var v struct {
		Errors map[string]Mapping `json:"error_mapping"`
	}
	b, _ := files.ReadFile("schemas/results.json")
	json.Unmarshal(b, &v)
	if m, ok := v.Errors[code]; ok {
		return m
	}
	return v.Errors["internal_error"]
}

// callSchema is shared by discovery and the write boundary. Business input
// validation still runs after successful-replay lookup in Store.Apply.
func callSchema(write bool) map[string]any {
	b, e := files.ReadFile("schemas/call.json")
	if e != nil {
		panic(e)
	}
	var schema map[string]any
	if e = json.Unmarshal(b, &schema); e != nil {
		panic(e)
	}
	if write {
		schema["required"] = []string{"input", "expected_version", "idempotency_key"}
	}
	return schema
}
func IdempotencyTTL() time.Duration {
	once.Do(initSchemas)
	return time.Duration(idempotencyHours) * time.Hour
}

// ChatInvalidField identifies only the top-level field, never schema error
// text (which may contain message content, credentials, or request values).
func ChatInvalidField(input json.RawMessage) string {
	once.Do(initSchemas)
	value, err := Decode(input)
	if err != nil {
		return ""
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		property, known := validators["chat"].Properties[key]
		if !known {
			// Bound attacker-controlled names and exclude control characters.
			if len(key) > 64 {
				return "unknown field"
			}
			for _, c := range key {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
					return "unknown field"
				}
			}
			return strconv.Quote(key)
		}
		if property.Validate(obj[key]) != nil {
			return strconv.Quote(key)
		}
	}
	return ""
}

// ValidChatOutputLimit uses the published constraint for either output alias.
func ValidChatOutputLimit(input json.RawMessage) bool {
	once.Do(initSchemas)
	value, err := Decode(input)
	return err == nil && validators["chat"].Properties["max_completion_tokens"].Validate(value) == nil
}
