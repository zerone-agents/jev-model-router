package management

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
)

func TestHealthManagementDiscovery(t *testing.T) {
	svc := New(nil, nil)
	found := false
	for _, c := range svc.Catalog() {
		if c.ID == "models.list" {
			var schema map[string]any
			json.Unmarshal(c.OutputSchema, &schema)
			b, _ := json.Marshal(schema)
			if !containsHealth(string(b)) {
				t.Fatal("health missing from machine schema")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("models.list absent")
	}
	_ = context.Background()
	_ = health.Unknown
	_ = routing.Model{}
}
func containsHealth(s string) bool {
	for i := 0; i+8 <= len(s); i++ {
		if s[i:i+8] == `"health"` {
			return true
		}
	}
	return false
}
