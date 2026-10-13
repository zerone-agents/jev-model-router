package httptransport

import (
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"testing"
)

func TestChatSemanticCommit(t *testing.T) {
	for _, d := range []routing.Delta{{Role: "assistant"}, {Content: json.RawMessage(`""`)}} {
		if chatSemanticEvent(routing.Event{Choices: []routing.Choice{{Delta: &d}}}) {
			t.Fatal("metadata committed")
		}
	}
	v := "thinking"
	d := routing.Delta{ReasoningContent: &v}
	if !chatSemanticEvent(routing.Event{Choices: []routing.Choice{{Delta: &d}}}) {
		t.Fatal("reasoning not committed")
	}
}
func TestPreambleBounds(t *testing.T) {
	b := preambleBudget{}
	for i := 0; i < 128; i++ {
		if b.add(512) != nil {
			t.Fatal(i)
		}
	}
	if b.add(0) == nil {
		t.Fatal("frame overflow accepted")
	}
	b = preambleBudget{}
	if b.add(65537) == nil {
		t.Fatal("byte overflow accepted")
	}
}
