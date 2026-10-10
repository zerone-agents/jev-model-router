package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"sync"
	"testing"
)

func TestMessagesProjectionPrivacyAndEstimate(t *testing.T) {
	raw := `{"model":"auto","max_tokens":2048,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"PRIVATE","signature":"SIGNATURE"},{"type":"redacted_thinking","data":"REDACTED"},{"type":"tool_use","id":"c","name":"f","input":{"secret":"ARGUMENT"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c","content":"RESULT"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`
	p, err := PrepareMessages([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	projection := p.Projection()
	if err = routing.ValidateRequest(projection); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(projection)
	for _, secret := range []string{"PRIVATE", "SIGNATURE", "REDACTED", "ARGUMENT", "RESULT", "AAAA"} {
		if bytes.Contains(wire, []byte(secret)) {
			t.Fatal("projection leaked", secret)
		}
	}
	if p.Summary() != "" {
		t.Fatal("summary leaked")
	}
	estimate, exact, err := p.Estimate(nativeMessagesTarget())
	if err != nil || exact || estimate.OutputReserve != 2048 || estimate.ImageTokens != 8192 || estimate.TotalTokens != estimate.InputTokens+2048 {
		t.Fatalf("%+v %v", estimate, err)
	}
	other, _ := PrepareMessages([]byte(strings.ReplaceAll(raw, "SIGNATURE", strings.Repeat("S", 1000))))
	larger, _, _ := other.Estimate(nativeMessagesTarget())
	otherWire, _ := json.Marshal(other.Projection())
	if larger.TotalTokens <= estimate.TotalTokens || !bytes.Equal(otherWire, wire) {
		t.Fatal("complete estimate must be independent of projection")
	}
	projection.Messages[0].Content = []byte(`"changed"`)
	fresh, _ := json.Marshal(p.Projection())
	if !bytes.Equal(fresh, wire) {
		t.Fatal("mutable projection")
	}
}
func TestMessagesPlanningIsolation(t *testing.T) {
	snapshot := routing.Snapshot{Providers: []routing.Provider{{ID: "native", Protocol: routing.ProtocolAnthropic}, {ID: "chat"}}, Models: []routing.Model{{ID: "n", ProviderID: "native", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}}, {ID: "o", ProviderID: "chat", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}}}}
	planner := &routing.Planner{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			model := "auto"
			extra := `,"thinking":{"type":"enabled","budget_tokens":1024}`
			want := "n"
			if i%2 == 0 {
				model = "o"
				extra = ""
				want = "o"
			}
			p, err := PrepareMessages([]byte(`{"model":"` + model + `","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]` + extra + `}`))
			if err != nil {
				t.Error(err)
				return
			}
			plan, err := planner.PlanPreparedWithEstimate(context.Background(), snapshot, p.Projection(), p.Check, p.Estimate)
			if err != nil || plan.ModelID != want {
				t.Errorf("%s %+v %v", want, plan, err)
			}
		}(i)
	}
	wg.Wait()
}
