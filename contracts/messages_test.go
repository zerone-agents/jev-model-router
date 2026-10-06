package contracts

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestMessagesSchema(t *testing.T) {
	for _, tc := range []struct {
		fields string
		ok     bool
	}{{`"max_tokens":16`, true}, {`"max_tokens":10000000`, true}, {`"max_tokens":15`, false}, {`"max_tokens":null`, false}, {`"max_tokens":"1024"`, false}, {`"max_tokens":1024,"thinking":{"type":"adaptive"}`, true}, {`"max_tokens":1024,"thinking":{"type":"enabled","budget_tokens":1024}`, true}, {`"max_tokens":1024,"thinking":{"type":"enabled"}`, false}, {`"max_tokens":1024,"fallbacks":["other"]`, false}, {`"max_tokens":1024,"output_config":{"effort":"max"}`, true}} {
		body := json.RawMessage(fmt.Sprintf(`{"model":"auto","messages":[{"role":"user","content":"query"}],%s}`, tc.fields))
		if got := Validate("messages", body) == nil; got != tc.ok {
			t.Fatalf("%s valid=%t want %t", tc.fields, got, tc.ok)
		}
	}
}
