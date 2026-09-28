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
)

//go:embed management.json schemas/*.json
var files embed.FS

type Capability struct {
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

func initSchemas() {
	b, e := files.ReadFile("management.json")
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(b, &caps); e != nil {
		panic(e)
	}
	validators = map[string]*jsonschema.Schema{}
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
