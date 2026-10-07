package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func (g *bifrostGenerator) CompleteMessages(ctx context.Context, t routing.Target, p *PreparedMessages) (json.RawMessage, error) {
	if err := p.Check(t); err != nil {
		return nil, err
	}
	client, release, err := g.acquire(t)
	if err != nil {
		return nil, err
	}
	defer release()
	bc := schemas.NewBifrostContext(ctx, time.Time{})
	defer bc.Cancel()
	bc.SetValue(schemas.BifrostContextKeyAllowPerRequestRawOverride, true)
	bc.SetValue(schemas.BifrostContextKeySendBackRawResponse, true)
	r, err := encode(bc, t, p.Projection())
	if err != nil {
		return nil, err
	}
	out, failure := client.ChatCompletionRequest(bc, r)
	if failure != nil {
		return nil, providerError(ctx, failure)
	}
	if out == nil || len(out.Choices) != 1 || out.Choices[0].Message == nil {
		return nil, routing.Fail("upstream_error", "invalid generation result")
	}
	choice := out.Choices[0]
	hasStop := choice.StopString != nil && *choice.StopString != ""
	if err = validMessagesFinish(choice.FinishReason, p.stops && !hasStop); err != nil {
		return nil, err
	}
	if choice.Message.ChatAssistantMessage != nil {
		for _, call := range choice.Message.ToolCalls {
			if call.ID == nil || *call.ID == "" || call.Function.Name == nil || *call.Function.Name == "" {
				return nil, routing.Fail("upstream_error", "invalid upstream tool call")
			}
			if err = validateToolArguments(call.Function.Arguments); err != nil {
				return nil, err
			}
		}
	}
	usage, err := reportedMessagesUsage(out.ExtraFields.RawResponse)
	if err != nil {
		return nil, err
	}
	if _, err = reportedMessagesStop(out.ExtraFields.RawResponse); err != nil {
		return nil, err
	}
	if hasStop && *choice.FinishReason != "stop" {
		return nil, routing.Fail("upstream_error", "upstream stop metadata contradicts finish reason")
	}
	out.ExtraFields = schemas.BifrostResponseExtraFields{}
	result := anthropic.ToAnthropicResponsesResponse(bc, out.ToBifrostResponsesResponse())
	result.Model = t.Model.ID
	if hasStop {
		result.StopReason = anthropic.AnthropicStopReason("stop_sequence")
		result.StopSequence = choice.StopString
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, routing.Fail("upstream_error", "cannot encode Messages response")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	delete(fields, "extra_fields")
	if usage == nil {
		delete(fields, "usage")
	} else {
		fields["usage"], _ = json.Marshal(publicMessagesUsage(usage))
	}
	return json.Marshal(fields)
}

// Parse the captured upstream usage, not Bifrost's synthetic aggregate. A
// missing member is not a reported zero and cannot become a normal usage value.
func reportedMessagesUsage(raw any) (*routing.Usage, error) {
	var data []byte
	if s, ok := raw.(string); ok {
		data = []byte(s)
	} else {
		data, _ = json.Marshal(raw)
	}
	d := json.NewDecoder(bytesReader(data))
	var found *routing.Usage
	for {
		var frame struct {
			Usage *struct {
				Input   *int64                     `json:"prompt_tokens"`
				Output  *int64                     `json:"completion_tokens"`
				Details map[string]json.RawMessage `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if d.Decode(&frame) != nil {
			break
		}
		if frame.Usage != nil && frame.Usage.Input != nil && frame.Usage.Output != nil && *frame.Usage.Input >= 0 && *frame.Usage.Output >= 0 {
			found = &routing.Usage{InputTokens: *frame.Usage.Input, OutputTokens: *frame.Usage.Output}
			cache := map[string]int64{}
			for _, group := range [][]string{{"cached_read_tokens", "cached_tokens"}, {"cached_write_tokens", "cache_write_tokens"}} {
				for _, key := range group {
					if value, ok := frame.Usage.Details[key]; ok && string(value) != "null" {
						var n int64
						if json.Unmarshal(value, &n) != nil || n < 0 {
							return nil, routing.Fail("upstream_error", "invalid upstream cache usage")
						}
						if prev, exists := cache[group[0]]; exists && prev != n {
							return nil, routing.Fail("upstream_error", "conflicting upstream cache usage")
						}
						cache[group[0]] = n
					}
				}
			}
			read, write := cache["cached_read_tokens"], cache["cached_write_tokens"]
			if read > found.InputTokens || write > found.InputTokens-read {
				return nil, routing.Fail("upstream_error", "upstream cache usage exceeds input usage")
			}
			found.InputDetails, _ = json.Marshal(cache)
		}
	}
	return found, nil
}

func publicMessagesUsage(usage *routing.Usage) map[string]int64 {
	out := map[string]int64{}
	if usage == nil {
		return out
	}
	cache := map[string]int64{}
	_ = json.Unmarshal(usage.InputDetails, &cache)
	out["input_tokens"] = usage.InputTokens - cache["cached_read_tokens"] - cache["cached_write_tokens"]
	out["output_tokens"] = usage.OutputTokens
	if n, ok := cache["cached_read_tokens"]; ok {
		out["cache_read_input_tokens"] = n
	}
	if n, ok := cache["cached_write_tokens"]; ok {
		out["cache_creation_input_tokens"] = n
	}
	return out
}

// Preserve only source-verified stop metadata. Never let it hide a truncation
// or tool terminal, even if a converter would otherwise prefer the string.
func reportedMessagesStop(raw any) (*string, error) {
	var data []byte
	if s, ok := raw.(string); ok {
		data = []byte(s)
	} else {
		data, _ = json.Marshal(raw)
	}
	d := json.NewDecoder(bytesReader(data))
	var stop *string
	for {
		var frame struct {
			Choices []struct {
				Finish *string `json:"finish_reason"`
				Stop   *string `json:"stop"`
			}
		}
		if d.Decode(&frame) != nil {
			break
		}
		for _, c := range frame.Choices {
			if c.Stop != nil && *c.Stop != "" {
				if c.Finish == nil || *c.Finish != "stop" {
					return nil, routing.Fail("upstream_error", "upstream stop metadata contradicts finish reason")
				}
				stop = c.Stop
			}
		}
	}
	return stop, nil
}
