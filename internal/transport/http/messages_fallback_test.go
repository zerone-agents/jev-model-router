package httptransport

import (
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"testing"
)

func TestMessagesFallbackSemantic(t *testing.T) {
	for _, v := range []struct {
		typ, body string
		want      bool
	}{{"message_start", `{"type":"message_start"}`, false}, {"content_block_start", `{"content_block":{"type":"text","text":""}}`, false}, {"content_block_start", `{"content_block":{"type":"tool_use","id":"a","name":"run"}}`, true}, {"content_block_delta", `{"delta":{"type":"thinking_delta","thinking":"hmm"}}`, true}} {
		if got := messagesSemanticEvent(provider.MessageEvent{Type: v.typ, Data: []byte(v.body)}); got != v.want {
			t.Fatal(v)
		}
	}
}
