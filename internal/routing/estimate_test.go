package routing

import (
	"encoding/json"
	"testing"
)

func TestSemanticEstimateIgnoresWireEncoding(t *testing.T) {
	var a, b Request
	json.Unmarshal([]byte(`{"model":"a","messages":[{"role":"user","content":"<你好🙂>"}],"max_completion_tokens":16}`), &a)
	json.Unmarshal([]byte(`{"model":"a-longer-name","messages":[{"role":"user","content":"\u003c你好🙂\u003e"}],"stream":true,"temperature":0.5,"max_completion_tokens":16}`), &b)
	x, _, _ := EstimateContext(Model{}, a)
	y, _, _ := EstimateContext(Model{}, b)
	if x != y {
		t.Fatalf("wire options changed estimate: %d vs %d", x, y)
	}
	if x != 23 {
		t.Fatalf("want ceil(12/4)+4+16=23 got %d", x)
	}
}

func TestEstimateComponents(t *testing.T) {
	var r Request
	if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"system","content":"abcd"},{"role":"developer","content":"你好"},{"role":"user","content":[{"type":"text","text":"🙂"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},{"role":"assistant","reasoning_content":"abcd","refusal":"abcd","tool_calls":[{"id":"1234","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"1234","content":"abcdefgh"}],"tools":[{"type":"function","function":{"name":"read","description":"abcd","parameters":{"type":"object"}}}],"response_format":{"type":"text"},"max_completion_tokens":16}`), &r); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(r)
	e, err := EstimateRequestContext(r)
	if err != nil {
		t.Fatal(err)
	}
	// 50 semantic text bytes, 2 argument + 17 schema + 15 response-format bytes.
	if e.TextBytes != 50 || e.StructuredBytes != 34 || e.FramingTokens != 36 || e.ImageTokens != 8192 || e.InputTokens != 8275 || e.TotalTokens != 8291 {
		t.Fatalf("%+v", e)
	}
	r.Messages[2].Content = json.RawMessage(`[{"type":"text","text":"🙂"},{"type":"image_url","image_url":{"url":"https://example.com/a-much-longer-url","detail":"high"}}]`)
	other, err := EstimateRequestContext(r)
	if err != nil || other != e {
		t.Fatalf("image encoding changed estimate %+v %v", other, err)
	}
	// The estimate itself never changes original content.
	var original Request
	json.Unmarshal(before, &original)
	EstimateRequestContext(original)
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("request mutated")
	}
}
func TestEstimateOutputAndSaturation(t *testing.T) {
	_, r := planFixture()
	e, err := EstimateRequestContext(r)
	if err != nil || e.OutputReserve != 4096 || e.InputTokens != 5 {
		t.Fatalf("%+v %v", e, err)
	}
	if r.Options["max_completion_tokens"] != nil {
		t.Fatal("injected output cap")
	}
	if saturatedSum(9223372036854775800, 100) != 9223372036854775807 {
		t.Fatal("overflow")
	}
}
