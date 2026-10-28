package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
			extra := `,"system":[{"type":"text","text":"native","cache_control":{"type":"ephemeral"}}]`
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

func TestMessagesNativeProjectionBounds(t *testing.T) {
	makeBody := func(messages []map[string]any, system string) []byte {
		raw, _ := json.Marshal(map[string]any{"model": "auto", "max_tokens": 2048, "system": system, "messages": messages})
		return raw
	}
	long := []map[string]any{{"role": "user", "content": strings.Repeat("a", 600000)}, {"role": "user", "content": strings.Repeat("b", 600000)}}
	many := make([]map[string]any, 2048)
	for i := range many {
		many[i] = map[string]any{"role": "user", "content": "x"}
	}
	var expanded []map[string]any
	for turn := 0; turn < 16; turn++ {
		var calls, results []map[string]any
		for i := 0; i < 128; i++ {
			id := fmt.Sprintf("t%d_%d", turn, i)
			calls = append(calls, map[string]any{"type": "tool_use", "id": id, "name": "f", "input": map[string]any{}})
			results = append(results, map[string]any{"type": "tool_result", "tool_use_id": id, "content": "PRIVATE"})
		}
		expanded = append(expanded, map[string]any{"role": "assistant", "content": calls}, map[string]any{"role": "user", "content": results})
	}
	snapshot := routing.Snapshot{Providers: []routing.Provider{{ID: "native", Protocol: routing.ProtocolAnthropic}, {ID: "chat"}}, Models: []routing.Model{{ID: "native", ProviderID: "native", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000000, Tools: true}}, {ID: "chat", ProviderID: "chat", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000000, Tools: true}}}}
	for name, body := range map[string][]byte{"long_users": makeBody(long, "system"), "system_plus_2048": makeBody(many, "system"), "tool_expansion": makeBody(expanded, "system")} {
		t.Run(name, func(t *testing.T) {
			p, err := PrepareMessages(body)
			if err != nil {
				t.Fatal(err)
			}
			if err = p.Check(nativeMessagesTarget()); err != nil {
				t.Fatal(err)
			}
			if name == "long_users" && len(p.Projection().Messages) != 3 {
				t.Fatal("native boundaries merged")
			}
			for _, model := range []string{"auto", "native"} {
				projection := p.Projection()
				projection.Model = model
				s := snapshot
				if model == "auto" {
					s.Models = s.Models[:1]
				}
				plan, err := (&routing.Planner{}).PlanPreparedWithEstimate(context.Background(), s, projection, p.Check, p.Estimate)
				if err != nil || plan.ModelID != "native" {
					t.Fatalf("%s: %+v %v", model, plan, err)
				}
			}
		})
	}
}
func TestMessagesChatIncompatibilityIsCandidateLocal(t *testing.T) {
	for _, content := range []string{
		`[{"role":"assistant","content":[{"type":"image","source":{"type":"url","url":"https://image.invalid/p"}}]},{"role":"user","content":"describe"}]`,
		`[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"f","input":{}}]},{"role":"user","content":[{"type":"text","text":"note"},{"type":"tool_result","tool_use_id":"t","content":"ok"}]}]`,
	} {
		p, err := PrepareMessages([]byte(`{"model":"auto","max_tokens":2048,"messages":` + content + `}`))
		if err != nil {
			t.Fatal(err)
		}
		err = p.Check(target("https://chat.invalid/v1"))
		if err == nil || !strings.Contains(err.Error(), "unsupported_request") {
			t.Fatalf("not candidate local: %v", err)
		}
		if err = p.Check(nativeMessagesTarget()); err != nil {
			t.Fatal(err)
		}
	}
}
