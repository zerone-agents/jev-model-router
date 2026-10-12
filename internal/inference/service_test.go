package inference

import (
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
)

func TestPlanMetadataMatchesRecord(t *testing.T) {
	r := &Request{record: routing.Record{RequestID: "r", ModelID: "m", Path: "explicit", ConfigVersion: 3, DecisionMillis: 12}}
	m := r.Metadata()
	if m.RequestID != "r" || m.ModelID != "m" || m.Path != "explicit" || m.ConfigVersion != 3 || m.DecisionMillis != 12 {
		t.Fatalf("%+v", m)
	}
	m.ModelID = "other"
	if r.Metadata().ModelID != "m" {
		t.Fatal("mutated record")
	}
}
