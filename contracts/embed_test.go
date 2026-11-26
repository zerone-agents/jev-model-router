package contracts

import (
	"encoding/json"
	"testing"
)

func TestReservedModelIDSchema(t *testing.T) {
	if Validate("models.put", json.RawMessage(`{"id":"auto","provider_id":"p","upstream_name":"x","enabled":false,"description":"","location":"cloud","capabilities":{"context_limit":4096,"tools":false,"images":false,"structured_output":false}}`)) == nil {
		t.Fatal("reserved ID accepted by schema")
	}
}
func TestModelTestRejectsMessages(t *testing.T) {
	if Validate("models.test", json.RawMessage(`{"id":"fast","messages":[]}`)) == nil {
		t.Fatal("arbitrary generation allowed")
	}
	if e := Validate("models.test", json.RawMessage(`{"id":"fast"}`)); e != nil {
		t.Fatal(e)
	}
}
func TestCatalogSchemasAndExamples(t *testing.T) {
	for _, c := range Catalog() {
		if c.ID == "" || len(c.InputSchema) == 0 || len(c.OutputSchema) == 0 || len(c.Examples) == 0 {
			t.Fatalf("incomplete capability %s", c.ID)
		}
		for _, e := range c.Examples {
			if err := Validate(c.ID, e); err != nil {
				t.Fatalf("%s example: %v", c.ID, err)
			}
		}
	}
}

func TestDiscoveredCallSchemasValidateRequests(t *testing.T) {
	for _, c := range Catalog() {
		schema := compile(c.Call.Schema)
		for _, example := range c.Examples {
			input, e := Decode(example)
			if e != nil {
				t.Fatal(e)
			}
			request := map[string]any{"input": input}
			if c.Write {
				request["expected_version"] = 1
				request["idempotency_key"] = "operation"
			}
			if e = schema.Validate(request); e != nil {
				t.Fatalf("%s complete request: %v", c.ID, e)
			}
			if c.Write {
				delete(request, "idempotency_key")
				if schema.Validate(request) == nil {
					t.Fatalf("%s accepts missing idempotency key", c.ID)
				}
			}
		}
	}
}

func TestChatAndInspectionOutputLimitContracts(t *testing.T) {
	for _, fields := range []struct {
		json  string
		valid bool
	}{
		{`"max_tokens":16`, true},
		{`"max_completion_tokens":10000000`, true},
		{`"max_tokens":1024,"max_completion_tokens":1024`, false},
		{`"max_tokens":null`, false},
		{`"max_tokens":15`, false},
		{`"max_tokens":10000001`, false},
	} {
		body := json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}],` + fields.json + `}`)
		for _, id := range []string{"chat", "route.inspect"} {
			if valid := Validate(id, body) == nil; valid != fields.valid {
				t.Fatalf("%s %s: valid=%t", id, body, valid)
			}
		}
	}
}

func TestChatSchemaViolationNested(t *testing.T) {
	for _, tc := range []struct{ input, path, rule, actual string }{
		{`{"model":"auto","messages":[{"role":"user","content":[{"type":null}]}]}`, "messages[0].content[0].type", "const", "null"},
		{`{"model":"auto","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":null}}]}`, "tools[0].function.name", "type", "null"},
		{`{"model":"auto","messages":[{"content":"hello"}]}`, "messages[0].role", "required", "missing"},
	} {
		d := ChatSchemaViolation(json.RawMessage(tc.input))
		if d == nil || d.Path != tc.path || d.Rule != tc.rule || d.Actual != tc.actual {
			t.Errorf("%s: got %+v", tc.input, d)
		}
	}
}
