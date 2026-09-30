package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedProviderWriteSchema(t *testing.T) {
	for _, tc := range []struct {
		fields string
		valid  bool
	}{
		{`"api_key":"test-secret"`, true}, {`"secret_ref":"env:KEY"`, true}, {`"secret_ref":"file:/run/key"`, true}, {`"secret_ref":"managed:` + strings.Repeat("a", 32) + `"`, true},
		{`"api_key":""`, false}, {`"api_key":"key","secret_ref":"env:KEY"`, false}, {`"description":"none"`, false},
	} {
		input := json.RawMessage(`{"id":"p","base_url":"https://example.com/v1",` + tc.fields + `}`)
		if err := Validate("providers.put", input); (err == nil) != tc.valid {
			t.Errorf("valid=%v: %v", tc.valid, err)
		}
	}
	if Validate("decision.put", json.RawMessage(`{"base_url":"https://example.com","model":"jev","secret_ref":"managed:`+strings.Repeat("a", 32)+`"}`)) == nil {
		t.Fatal("managed decision ref accepted")
	}
	for _, c := range Catalog() {
		if c.ID == "providers.put" {
			if strings.Contains(string(c.OutputSchema), `"api_key"`) {
				t.Fatal("write-only key in output")
			}
			if !strings.Contains(string(c.InputSchema), `"writeOnly":true`) && !strings.Contains(string(c.InputSchema), `"writeOnly": true`) {
				t.Fatal("writeOnly missing")
			}
		}
	}
}
func TestManagedKeyByteLimit(t *testing.T) {
	for _, tc := range []struct {
		k  string
		ok bool
	}{{strings.Repeat("a", 16384), true}, {strings.Repeat("中", 5462), false}, {"key\nline", false}, {"key\x00", false}} {
		b, _ := json.Marshal(map[string]string{"id": "p", "base_url": "https://example.com", "api_key": tc.k})
		if err := Validate("providers.put", b); (err == nil) != tc.ok {
			t.Errorf("bytes=%d expected valid=%v", len(tc.k), tc.ok)
		}
	}
}
