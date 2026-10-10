package contracts

import (
	"encoding/json"
	"testing"
)

func TestPlaygroundContract(t *testing.T) {
	var p map[string]json.RawMessage
	if e := json.Unmarshal(Catalog()[0].Call.Protocol, &p); e != nil {
		t.Fatal(e)
	}
	if len(p["playground"]) == 0 {
		t.Fatal("missing playground discovery")
	}
	if e := Validate("playground", json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"stream":true}`)); e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{`{"model":"auto","messages":[{"role":"system","content":"hi"}],"stream":true}`, `{"model":"auto","messages":[{"role":"user","content":"hi","reasoning_content":"secret"}],"stream":true}`} {
		if Validate("playground", json.RawMessage(b)) == nil {
			t.Fatal("accepted invalid request")
		}
	}
}
