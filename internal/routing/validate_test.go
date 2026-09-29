package routing

import (
	"encoding/json"
	"testing"
)

func fixture() Snapshot {
	return Snapshot{Version: 1, Providers: []Provider{{ID: "p", BaseURL: "https://example.com/v1", SecretRef: "env:KEY"}}, Models: []Model{{ID: "fast", ProviderID: "p", UpstreamName: "auto", Enabled: true, Location: "cloud", Capabilities: Capabilities{ContextLimit: 4096}}}, Prompt: "balanced"}
}
func TestReservedAutoID(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := fixture()
		s.Models[0].ID = "auto"
		s.Models[0].Enabled = enabled
		if ValidateSnapshot(s) == nil {
			t.Fatal("auto accepted")
		}
	}
}
func TestUpstreamNameMayBeAuto(t *testing.T) {
	if e := ValidateSnapshot(fixture()); e != nil {
		t.Fatal(e)
	}
}
func TestProviderReferenceRequired(t *testing.T) {
	s := fixture()
	s.Models[0].ProviderID = "missing"
	if ValidateSnapshot(s) == nil {
		t.Fatal("dangling reference")
	}
}
func TestChatRejectsUnknownControlFields(t *testing.T) {
	for _, body := range []string{`{"model":"auto","messages":[{"role":"user","content":"hi"}],"fallbacks":["x"]}`, `{"model":"auto","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":8}`, `{"model":"auto","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]}`} {
		var r Request
		if e := json.Unmarshal([]byte(body), &r); e == nil && ValidateRequest(r) == nil {
			t.Fatalf("unsupported input accepted %s", body)
		}
	}
}
func TestRequestRoundTrip(t *testing.T) {
	body := `{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"你好"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}],"temperature":0,"max_completion_tokens":32}`
	var r Request
	if e := json.Unmarshal([]byte(body), &r); e != nil {
		t.Fatal(e)
	}
	if e := ValidateRequest(r); e != nil {
		t.Fatal(e)
	}
	if string(r.Options["temperature"]) != "0" {
		t.Fatal("zero option lost")
	}
}
