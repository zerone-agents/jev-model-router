package provider

import (
	"context"
	"encoding/json"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
	"testing"
	"time"
)

func TestAnthropicUpstreamRequestContract(t *testing.T) {
	tool := `,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]`
	for _, tc := range []struct {
		name, extra string
		ok          bool
		thinking    string
	}{
		{"default", "", true, ""}, {"limit", `,"max_tokens":123`, true, ""},
		{"adaptive", `,"chat_template_kwargs":{"enable_thinking":true}`, true, "adaptive"},
		{"disabled", `,"chat_template_kwargs":{"enable_thinking":false}`, true, "disabled"},
		{"none", `,"reasoning_effort":"none"`, true, "disabled"},
		{"effort", `,"reasoning_effort":"high"`, true, ""},
		{"minimal", `,"reasoning_effort":"minimal"`, false, ""},
		{"xhigh", `,"reasoning_effort":"xhigh"`, false, ""},
		{"conflict", `,"reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":true}`, false, ""},
		{"tool_none", tool + `,"reasoning_effort":"none"`, true, "disabled"},
		{"tool_false", tool + `,"chat_template_kwargs":{"enable_thinking":false}`, true, "disabled"},
		{"tool_unspecified", tool, true, ""},
		{"tool_thinking", tool + `,"chat_template_kwargs":{"enable_thinking":true}`, true, "adaptive"},
		{"sampling", `,"temperature":0.5`, true, ""}, {"two_sampling", `,"temperature":0.5,"top_p":0.9`, false, ""},
		{"stop", `,"stop":["end"]`, true, ""}, {"seed", `,"seed":1`, false, ""},
		{"json_object", `,"response_format":{"type":"json_object"}`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"}]`+tc.extra+`}`)
			target := nativeTarget("https://example.com/v1")
			e := Check(target, r)
			if !tc.ok {
				if e == nil {
					t.Fatal("accepted unrepresentable input")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			bc := schemas.NewBifrostContext(context.Background(), time.Time{})
			defer bc.Cancel()
			in, e := encode(bc, target, r)
			if e != nil {
				t.Fatal(e)
			}
			wire := in.RawRequestBody
			var b map[string]any
			json.Unmarshal(wire, &b)
			if tc.name == "default" && b["max_tokens"] != float64(65536) {
				t.Fatalf("default: %s", wire)
			}
			if tc.name == "limit" && b["max_tokens"] != float64(123) {
				t.Fatalf("explicit limit: %s", wire)
			}
			if tc.thinking != "" {
				if v, ok := b["thinking"].(map[string]any); !ok || v["type"] != tc.thinking {
					t.Fatalf("thinking: %s", wire)
				}
			}
			if tc.name == "effort" {
				if v, ok := b["output_config"].(map[string]any); !ok || v["effort"] != "high" || b["thinking"] != nil {
					t.Fatalf("effort: %s", wire)
				}
			}
		})
	}
	for _, body := range []string{
		`{"model":"fast","messages":[{"role":"user","content":"hi"},{"role":"system","content":"late"}]}`,
	} {
		if Check(nativeTarget("https://example.com"), req(t, body)) == nil {
			t.Fatal("unsafe history accepted")
		}
	}
}

func TestAnthropicUpstreamMessageContract(t *testing.T) {
	for _, body := range []string{
		`{"model":"fast","messages":[{"role":"assistant","content":"hi","reasoning_content":"old"},{"role":"user","content":"next"}]}`,
		`{"model":"fast","messages":[{"role":"system","content":"first"},{"role":"developer","content":"second"},{"role":"user","content":"hi"}]}`,
		`{"model":"fast","messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`,
		`{"model":"fast","messages":[{"role":"user","content":"hi"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},{"role":"tool","tool_call_id":"c1","content":"found"}],"reasoning_effort":"none"}`,
		`{"model":"fast","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","strict":true,"schema":{"type":"object","properties":{"v":{"type":"string"}},"required":["v"],"additionalProperties":false}}}}`,
	} {
		if e := Check(nativeTarget("https://example.com"), req(t, body)); e != nil {
			t.Errorf("body %s: %v", body, e)
		}
	}
	for _, choice := range []string{`"auto"`, `"none"`, `"required"`, `{"type":"function","function":{"name":"lookup"}}`} {
		body := `{"model":"fast","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":` + choice + `,"parallel_tool_calls":false,"reasoning_effort":"none"}`
		if e := Check(nativeTarget("https://example.com"), req(t, body)); e != nil {
			bc := schemas.NewBifrostContext(context.Background(), time.Time{})
			defer bc.Cancel()
			in, _ := encodeOpenAI(bc, nativeTarget("https://example.com"), req(t, body))
			in.Provider = schemas.Anthropic
			raw, _ := anthropic.BuildAnthropicChatRequestBody(bc, in, anthropic.AnthropicRequestBuildConfig{Provider: schemas.Anthropic})
			t.Errorf("choice %s: %v wire %s", choice, e, raw)
		}
	}
}
