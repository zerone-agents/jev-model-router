package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"io"
	"sync"
	"time"
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
