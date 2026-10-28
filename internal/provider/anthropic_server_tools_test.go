package provider

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAnthropicServerToolsContinueStream(t *testing.T) {
	body := nativeStart() + nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"server-1","name":"webReader","input":{"url":"https://example.com"}}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`) + nativeFrame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_result","tool_use_id":"server-1","content":"page content"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":1}`) + nativeFrame("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`) + nativeFrame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"after server tool"}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":2}`) + nativeEnd()
	events, err := collectNative(t, body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		for _, c := range event.Choices {
			if c.Delta != nil {
				if len(c.Delta.ToolCalls) > 0 {
					t.Fatal("server tool exposed as client call")
				}
				if string(c.Delta.Content) == `"after server tool"` {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("missing text after server tool")
	}
}

func TestProjectServerTools(t *testing.T) {
	call := `{"type":"server_tool_use","id":"s","name":"webReader","input":{}}`
	result := `{"type":"tool_result","tool_use_id":"s","content":"page"}`
	text := `{"type":"text","text":"answer"}`
	for _, tc := range []struct {
		name, blocks string
		valid        bool
	}{
		{"valid", call + "," + result + "," + text, true},
		{"orphan", result + "," + text, false},
		{"duplicate", call + "," + result + "," + result, false},
		{"missing", call + "," + text, false},
		{"client_collision", call + `,{"type":"tool_use","id":"s","name":"client","input":{}}` + "," + result, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := projectServerTools([]byte(`{"content":[` + tc.blocks + `],"id":"m","usage":{"input_tokens":12}}`))
			var rawBlocks []json.RawMessage
			if err := json.Unmarshal([]byte("["+tc.blocks+"]"), &rawBlocks); err != nil {
				t.Fatal(err)
			}
			stream := nativeStart()
			for i, block := range rawBlocks {
				var b nativeBlock
				json.Unmarshal(block, &b)
				var delta string
				if b.Type == "text" {
					value, _ := json.Marshal(*b.Text)
					block = json.RawMessage(`{"type":"text","text":""}`)
					delta = nativeFrame("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, i, value))
				}
				stream += nativeFrame("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, i, block))
				stream += delta
				stream += nativeFrame("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
			}
			stream += nativeEnd()
			_, streamErr := collectNative(t, stream)
			if (streamErr == nil) != tc.valid {
				t.Fatalf("stream linkage validation: %v", streamErr)
			}
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid linkage accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var v struct {
				Content []nativeBlock `json:"content"`
				ID      string        `json:"id"`
				Usage   nativeUsage   `json:"usage"`
			}
			json.Unmarshal(out, &v)
			if len(v.Content) != 1 || v.Content[0].Type != "text" || v.ID != "m" || v.Usage.Input == nil || *v.Usage.Input != 12 {
				t.Fatal("projection lost content or metadata")
			}
		})
	}
}
