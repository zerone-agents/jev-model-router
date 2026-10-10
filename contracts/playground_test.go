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

func TestPlaygroundEventSchemas(t *testing.T) {
	for id, body := range map[string]string{"playground.route": `{"request_id":"r","model_id":"m","path":"explicit","config_version":1,"decision_ms":0}`, "playground.delta": `{"reasoning_content":"hidden"}`, "playground.done": `{"finish_reason":"stop"}`, "playground.error": `{"code":"timeout","message":"request timed out"}`} {
		if e := Validate(id, json.RawMessage(body)); e != nil {
			t.Fatalf("%s: %v", id, e)
		}
		if e := Validate(id, json.RawMessage(`{"unexpected":true}`)); e == nil {
			t.Fatal("accepted invalid event", id)
		}
	}
}
