package contracts

import (
	"encoding/json"
	"testing"
)

func TestSessionContract(t *testing.T) {
	for _, id := range []string{"session.login", "session.logout"} {
		if err := Validate(id, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{`null`, `{"token":"secret"}`, `{} {}`} {
			if Validate(id, json.RawMessage(body)) == nil {
				t.Fatalf("accepted %s", body)
			}
		}
	}
	if m := ErrorMapping("session_limit"); m.HTTP != 429 || m.Exit != 6 {
		t.Fatal(m)
	}
	for _, c := range Catalog() {
		var p map[string]json.RawMessage
		if err := json.Unmarshal(c.Call.Protocol, &p); err != nil {
			t.Fatal(err)
		}
		if len(p["session"]) == 0 {
			t.Fatal("missing session contract")
		}
	}
}
