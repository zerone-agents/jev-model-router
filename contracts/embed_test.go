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
