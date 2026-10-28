package provider

import (
	"bytes"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
)

func TestNativeEffortCopyAndPrecedence(t *testing.T) {
	for _, extra := range []string{``, `,"thinking":{"type":"disabled"}`, `,"thinking":{"type":"enabled","budget_tokens":1024}`, `,"output_config":{"effort":"high"}`} {
		raw := []byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"user","content":"hello"}]` + extra + `}`)
		p, err := PrepareMessages(raw)
		if err != nil {
			t.Fatal(err)
		}
		if (routing.ValidateAutoEffortControls(p.Projection()) == nil) != (extra == "") {
			t.Fatalf("precedence %s", extra)
		}
	}
	raw := []byte(`{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private","signature":"sig"},{"type":"text","text":"hi"}]},{"role":"user","content":"next"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`)
	p, err := PrepareMessages(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, err := p.WithEffectiveEffort("high")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.body, raw) {
		t.Fatal("original mutated")
	}
	var a, b map[string]json.RawMessage
	json.Unmarshal(raw, &a)
	json.Unmarshal(q.body, &b)
	if !bytes.Equal(a["messages"], b["messages"]) {
		t.Fatal("history changed")
	}
	var output map[string]json.RawMessage
	json.Unmarshal(b["output_config"], &output)
	if string(output["effort"]) != `"high"` || output["format"] == nil {
		t.Fatal("output lost")
	}
}
