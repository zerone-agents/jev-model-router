package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoEffortConfiguration(t *testing.T) {
	base := `{"id":"m","provider_id":"p","upstream_name":"m","description":"test","location":"cloud","enabled":true,"capabilities":{"context_limit":4096,"tools":false,"images":false,"structured_output":false},"auto_effort":{"allowed":%s}}`
	for _, tc := range []struct {
		levels string
		valid  bool
	}{{`["low","high"]`, false}, {`[]`, false}, {`["high","high"]`, false}, {`["none"]`, false}, {`["other"]`, false}} {
		raw := strings.Replace(base, "%s", tc.levels, 1)
		err := Validate("models.put", json.RawMessage(raw))
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.levels, err)
		}
	}
}
func TestInspectionRejectsNativeEffortEnvelope(t *testing.T) {
	for _, extra := range []string{`,"thinking":{"type":"enabled","budget_tokens":1024}`, `,"output_config":{"effort":"high"}`} {
		b := json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}]` + extra + `}`)
		if Validate("route.inspect", b) == nil {
			t.Fatal("native input accepted")
		}
	}
	b := json.RawMessage(`{"model":"auto","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"sig"}]}]}`)
	if Validate("route.inspect", b) == nil {
		t.Fatal("signed thinking accepted")
	}
}
