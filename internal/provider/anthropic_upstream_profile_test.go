package provider

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
)

func TestNativeSignatureOptionalProfile(t *testing.T) {
	for _, tc := range []struct {
		url, model string
		want       bool
	}{
		{"https://open.bigmodel.cn/api/anthropic/v1", "glm-5.3", true},
		{"https://open.bigmodel.cn:443/api/anthropic/v1/", "glm-5.3-flash", true},
		{"https://api.anthropic.com/v1", "glm-5.3", false},
		{"https://open.bigmodel.cn.example.com/api/anthropic/v1", "glm-5.3", false},
		{"https://proxy.example/api/anthropic/v1", "glm-5.3", false},
		{"http://open.bigmodel.cn/api/anthropic/v1", "glm-5.3", false},
		{"https://open.bigmodel.cn:8443/api/anthropic/v1", "glm-5.3", false},
		{"https://open.bigmodel.cn/api/other/v1", "glm-5.3", false},
		{"https://open.bigmodel.cn/api/anthropic/v1?mode=x", "glm-5.3", false},
		{"https://open.bigmodel.cn/api/anthropic/v1", "glm-5.1", false},
		{"https://open.bigmodel.cn/api/anthropic/v1", "claude-sonnet-4-5", false},
	} {
		target := nativeTarget(tc.url)
		target.Model.UpstreamName = tc.model
		if got := nativeSignatureOptional(target); got != tc.want {
			t.Errorf("%s %s: %v", tc.url, tc.model, got)
		}
	}
}

func TestNativeOptionalSignatureResponses(t *testing.T) {
	raw := []byte(`{"id":"m","type":"message","role":"assistant","model":"glm-5.3","content":[{"type":"thinking","thinking":"Need lookup.","signature":"provider-signature"},{"type":"tool_use","id":"c1","name":"lookup","input":{"q":"x"}}],"stop_reason":"tool_use"}`)
	var native anthropic.AnthropicMessageResponse
	if err := json.Unmarshal(raw, &native); err != nil {
		t.Fatal(err)
	}
	ctx := schemas.NewBifrostContext(context.Background(), time.Time{})
	defer ctx.Cancel()
	for _, optional := range []bool{false, true} {
		out, err := anthropicUpstreamCompletion(native.ToBifrostChatResponse(ctx), raw, true, optional)
		if !optional {
			if err == nil {
				t.Fatal("signed tools accepted without provider profile")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		m := out.Choices[0].Message
		if m.ReasoningContent == nil || *m.ReasoningContent != "Need lookup." || len(m.ToolCalls) != 1 || m.ToolCalls[0].ID != "c1" {
			t.Fatalf("content lost: %+v", m)
		}
	}
	for _, signatureAtStart := range []bool{false, true} {
		start := `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`
		if signatureAtStart {
			start = strings.Replace(start, `"thinking":""`, `"thinking":"","signature":"provider-signature"`, 1)
		}
		body := nativeStart() + nativeFrame("content_block_start", start) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need lookup."}}`)
		if !signatureAtStart {
			body += nativeFrame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"provider-signature"}}`)
		}
		body += nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`) + nativeFrame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c1","name":"lookup","input":{"q":"x"}}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":1}`) + strings.Replace(nativeEnd(), "end_turn", "tool_use", 1)
		for _, optional := range []bool{false, true} {
			state := &anthropicUpstreamValidation{tools: true, signatureOptional: optional}
			reader := newAnthropicUpstreamReader(strings.NewReader(body), state)
			var err error
			for err == nil {
				_, _, err = reader.ReadEvent()
			}
			if optional {
				if err != io.EOF || !state.terminalVerified {
					t.Fatalf("optional signature failed: %v", err)
				}
			} else if err == io.EOF || state.terminalVerified {
				t.Fatal("required signature silently discarded")
			}
		}
	}
}
