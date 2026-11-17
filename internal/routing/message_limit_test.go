package routing

import (
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/contracts"
	"strings"
	"testing"
)

func TestChatMessageLimit(t *testing.T) {
	max := contracts.ChatMessageLimit()
	for _, n := range []int{2049, max, max + 1} {
		body := json.RawMessage(`{"model":"auto","messages":[` + strings.TrimSuffix(strings.Repeat(`{"role":"user","content":"x"},`, n), ",") + `]}`)
		err := ValidateChatSchema(body)
		if n <= max && err != nil {
			t.Fatalf("%d rejected: %v", n, err)
		}
		if n > max && (err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d items; maximum is %d", n, max))) {
			t.Fatalf("%d incorrect error: %v", n, err)
		}
	}
}
