package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"reflect"
	"strings"
	"testing"
)

func TestPreparedCheckMatchesFullCheck(t *testing.T) {
	for _, body := range []string{
		`{"model":"auto","messages":[{"role":"user","content":"hello"}]}`,
		`{"model":"auto","messages":[{"role":"assistant","tool_calls":[{"id":"x","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"x","content":"result"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"tool_choice":"required"}`,
		`{"model":"auto","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`,
		`{"model":"auto","messages":[{"role":"assistant","content":"answer","reasoning_content":"reasoning"}],"reasoning_effort":"high","chat_template_kwargs":{"enable_thinking":true}}`,
		`{"model":"auto","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":16}`,
	} {
		var r routing.Request
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		if err := routing.ValidateRequest(r); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(r)
		check := PrepareCheck(r)
		for _, model := range []string{"gpt-4o-mini", "o3", "claude-fable-5.1", "deepseek-v4.1-flash"} {
			target := routing.Target{Model: routing.Model{UpstreamName: model, Capabilities: routing.Capabilities{Tools: true, Images: true, StructuredOutput: true}}}
			got, want := check(target), Check(target, r)
			if (got == nil) != (want == nil) {
				t.Fatalf("model=%s body=%s prepared=%v full=%v", model, body, got, want)
			}
		}
		after, _ := json.Marshal(r)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("request mutated")
		}
	}
}

func BenchmarkFallbackCompatibility(b *testing.B) {
	for _, size := range []int{256 * 1024, 1024 * 1024} {
		for _, count := range []int{1, 10, 30} {
			for _, prepared := range []bool{false, true} {
				b.Run(fmt.Sprintf("bytes=%d/models=%d/prepared=%t", size, count, prepared), func(b *testing.B) {
					content, _ := json.Marshal(strings.Repeat("context ", size/8))
					r := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: content}}}
					s := routing.Snapshot{Providers: []routing.Provider{{ID: "p"}}}
					for i := 0; i < count; i++ {
						s.Models = append(s.Models, routing.Model{ID: fmt.Sprint(i), ProviderID: "p", UpstreamName: "gpt-4o-mini", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: int64(100 + i)}})
					}
					p := routing.Planner{Check: Check}
					if prepared {
						p.Check = nil
						p.PrepareCheck = PrepareCheck
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						plan, err := p.Plan(context.Background(), s, r)
						if err != nil || plan.Path != "context_estimate_fallback" {
							b.Fatalf("%+v %v", plan, err)
						}
					}
				})
			}
		}
	}
}
