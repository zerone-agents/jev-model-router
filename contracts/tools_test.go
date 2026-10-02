package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSDKToolDescriptions(t *testing.T) {
	for _, size := range []int{4109, 4407, 65537} {
		body, _ := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "Bash", "description": strings.Repeat("x", size), "parameters": map[string]any{"type": "object"}}}}})
		for _, schema := range []string{"chat", "route.inspect"} {
			if err := Validate(schema, body); err != nil {
				t.Errorf("%s rejects %d character tool description: %v", schema, size, err)
			}
		}
	}
}
