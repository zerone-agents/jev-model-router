package compat

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	"github.com/maximhq/bifrost/core/schemas"
)

func anthropicContext(t *testing.T) *schemas.BifrostContext {
	t.Helper()
	c := schemas.NewBifrostContext(context.Background(), time.Time{})
	t.Cleanup(c.Cancel)
	return c
}

func anthropicWire(t *testing.T, body string) (map[string]any, *schemas.BifrostChatRequest) {
	t.Helper()
	c := anthropicContext(t)
	var in anthropic.AnthropicMessageRequest
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	r := in.ToBifrostResponsesRequest(c).ToChatRequest()
	r.Provider, r.Model, r.Fallbacks = schemas.OpenAI, "qwen3.8-flash", nil
	b, err := json.Marshal(openai.ToOpenAIChatRequest(c, r))
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	return wire, r
}

func TestAnthropicRequestConversion(t *testing.T) {
	for _, image := range []string{`{"type":"url","url":"https://example.invalid/private.png"}`, `{"type":"base64","media_type":"image/png","data":"aGVsbG8="}`} {
		wire, _ := anthropicWire(t, `{"model":"external","max_tokens":1024,"system":"rules","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":`+image+`}]}],"temperature":0.2,"top_p":0.8,"stop_sequences":["END"],"tools":[{"name":"lookup","description":"`+strings.Repeat("x", 5000)+`","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],"tool_choice":{"type":"any","disable_parallel_tool_use":true},"output_config":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`)
		for k, want := range map[string]any{"model": "qwen3.8-flash", "max_completion_tokens": float64(1024), "temperature": 0.2, "top_p": 0.8, "tool_choice": "required"} {
			if !reflect.DeepEqual(wire[k], want) {
				t.Errorf("%s: got %#v want %#v", k, wire[k], want)
			}
		}
		msgs := wire["messages"].([]any)
		if len(msgs) != 3 || msgs[0].(map[string]any)["content"] != "rules" || msgs[1].(map[string]any)["content"] != "describe" {
			t.Fatalf("system changed: %#v", msgs)
		}
		parts := msgs[2].(map[string]any)["content"].([]any)
		if len(parts) != 1 {
			t.Fatalf("image order changed: %#v", parts)
		}
		url := parts[0].(map[string]any)["image_url"].(map[string]any)["url"]
		wantURL := "https://example.invalid/private.png"
		if strings.Contains(image, "base64") {
			wantURL = "data:image/png;base64,aGVsbG8="
		}
		if url != wantURL {
			t.Errorf("image changed: %#v", url)
		}
		fn := wire["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		if len(fn["description"].(string)) != 5000 {
			t.Fatal("description truncated")
		}
		// Responses puts stop in ExtraParams; ordinary OpenAI marshaling does not
		// merge it. Production must use the final ExtraParams merge as well.
		if wire["stop"] != nil {
			t.Log("stop now survives ordinary serialization")
		}
		if wire["response_format"] == nil {
			t.Fatal("structured output dropped")
		}
		if wire["parallel_tool_calls"] != nil {
			t.Fatal("characterization changed: parallel flag now preserved")
		}
		t.Log("conversion splits text/image into adjacent user messages and drops disable_parallel_tool_use")
	}
}

func TestAnthropicThinkingConversion(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		budget       bool
		effort       string
	}{
		{"enabled", `"thinking":{"type":"enabled","budget_tokens":1024}`, true, ""},
		{"disabled", `"thinking":{"type":"disabled"}`, false, "none"},
		{"adaptive", `"thinking":{"type":"adaptive"}`, false, "high"},
		{"effort", `"output_config":{"effort":"max"}`, false, "max"},
		{"both", `"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"high"}`, true, "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, r := anthropicWire(t, `{"model":"external","max_tokens":4096,"messages":[{"role":"user","content":"test"}],`+tc.fields+`}`)
			if r.Params.Reasoning == nil {
				t.Fatal("reasoning lost")
			}
			if tc.budget && (r.Params.Reasoning.MaxTokens == nil || *r.Params.Reasoning.MaxTokens != 1024) {
				t.Fatal("neutral budget lost")
			}
			if tc.effort != "" && (r.Params.Reasoning.Effort == nil || *r.Params.Reasoning.Effort != tc.effort) {
				t.Fatalf("effort changed: %+v", r.Params.Reasoning)
			}
			if wire["budget_tokens"] != nil {
				t.Fatal("characterization changed: budget now reaches OpenAI wire")
			}
			t.Logf("wire reasoning_effort=%v; independent budget not transmitted", wire["reasoning_effort"])
		})
	}
}

func TestAnthropicToolHistoryConversion(t *testing.T) {
	wire, _ := anthropicWire(t, `{"model":"external","max_tokens":1024,"messages":[{"role":"user","content":"lookup"},{"role":"assistant","content":[{"type":"thinking","thinking":"previous thought","signature":""},{"type":"text","text":"checking"},{"type":"tool_use","id":"call1","name":"lookup","input":{"q":"value"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"result"},{"type":"text","text":"continue"}]}]}`)
	msgs := wire["messages"].([]any)
	var assistant, tool map[string]any
	var thought string
	for _, v := range msgs {
		m := v.(map[string]any)
		if m["role"] == "assistant" {
			assistant = m
			if s, ok := m["reasoning_content"].(string); ok {
				thought += s
			}
		}
		if m["role"] == "tool" {
			tool = m
		}
	}
	if assistant == nil || tool == nil {
		t.Fatalf("tool round lost: %#v", msgs)
	}
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "call1" || tool["tool_call_id"] != "call1" || tool["content"] != "result" {
		t.Fatalf("pairing lost: %#v", msgs)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call["function"].(map[string]any)["arguments"].(string)), &args); err != nil || args["q"] != "value" {
		t.Fatal("arguments changed")
	}
	if thought != "previous thought" {
		t.Fatalf("thinking history lost: %#v", assistant)
	}
}

func TestAnthropicResponseConversion(t *testing.T) {
	for _, finish := range []string{"stop", "length", "tool_calls"} {
		c := anthropicContext(t)
		var r schemas.BifrostChatResponse
		body := `{"id":"chat1","model":"qwen3.8-flash","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"thought"},"finish_reason":"` + finish + `"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		out := anthropic.ToAnthropicResponsesResponse(c, r.ToBifrostResponsesResponse())
		if out.Usage == nil || out.Usage.InputTokens != 7 || out.Usage.OutputTokens != 3 {
			t.Fatalf("usage: %+v", out.Usage)
		}
		want := map[string]string{"stop": "end_turn", "length": "max_tokens", "tool_calls": "tool_use"}[finish]
		if string(out.StopReason) != want {
			t.Fatalf("%s -> %s", finish, out.StopReason)
		}
		if len(out.Content) != 2 || string(out.Content[0].Type) != "thinking" || *out.Content[0].Thinking != "thought" {
			t.Fatalf("thinking output changed: %+v", out.Content)
		}
	}
}

func TestAnthropicStreamConversionCharacterization(t *testing.T) {
	c := anthropicContext(t)
	state := schemas.AcquireChatToResponsesStreamState()
	t.Cleanup(func() { schemas.ReleaseChatToResponsesStreamState(state) })
	var types []string
	var startUsage map[string]any
	for _, body := range []string{
		`{"id":"x","model":"qwen3.8-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thought"}}]}`,
		`{"id":"x","model":"qwen3.8-flash","choices":[{"index":0,"delta":{"content":"answer"}}]}`,
		`{"id":"x","model":"qwen3.8-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	} {
		var chunk schemas.BifrostChatResponse
		if err := json.Unmarshal([]byte(body), &chunk); err != nil {
			t.Fatal(err)
		}
		for _, response := range chunk.ToBifrostResponsesStreamResponse(state) {
			for _, event := range anthropic.ToAnthropicResponsesStreamResponse(c, response) {
				b, _ := json.Marshal(event)
				var m map[string]any
				json.Unmarshal(b, &m)
				types = append(types, m["type"].(string))
				if m["type"] == "message_start" {
					startUsage = m["message"].(map[string]any)["usage"].(map[string]any)
				}
			}
		}
	}
	if len(types) == 0 || types[0] != "message_start" || types[len(types)-1] != "message_stop" {
		t.Fatalf("events: %v", types)
	}
	if startUsage["input_tokens"] != float64(0) {
		t.Fatalf("initial fabricated usage changed: %#v", startUsage)
	}
	t.Logf("events=%v; initial usage=%v (synthesized before actual usage)", types, startUsage)
}
