package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeRequestPreservesToolNumbers(t *testing.T) {
	const args = `{"integer":9007199254740993,"negative":-9007199254740993,"decimal":0.1234567890123456789,"nested":[{"value":9007199254740993}],"equivalent":[1.0,1e0,-0.0,1000e-3]}`
	for _, streaming := range []bool{false, true} {
		for _, thinking := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/thinking=%v", streaming, thinking), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Messages []struct {
							Content []struct {
								Type  string          `json:"type"`
								Input json.RawMessage `json:"input"`
							} `json:"content"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					found := false
					for _, m := range body.Messages {
						for _, b := range m.Content {
							if b.Type == "tool_use" {
								found = true
								if string(b.Input) != args {
									t.Errorf("tool numbers changed: %s", b.Input)
								}
							}
						}
					}
					if !found {
						t.Error("tool history missing")
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, nativeStart()+nativeText()+nativeEnd())
					} else {
						fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`)
					}
				}))
				defer server.Close()
				extra := ""
				if thinking {
					extra = `,"reasoning_content":"Need lookup."`
				}
				r := req(t, fmt.Sprintf(`{"model":"fast","messages":[{"role":"user","content":"lookup"},{"role":"assistant"%s,"tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":%q}}]},{"role":"tool","tool_call_id":"c1","content":"found"}]}`, extra, args))
				r.Stream = streaming
				g := generator(t)
				if !streaming {
					if _, err := g.Complete(context.Background(), nativeTarget(server.URL), r); err != nil {
						t.Fatal(err)
					}
				} else {
					stream, err := g.Stream(context.Background(), nativeTarget(server.URL), r)
					if err != nil {
						t.Fatal(err)
					}
					defer stream.Close()
					for {
						_, err = stream.Next(context.Background())
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestNativeRequestRejectsRoundedSchema(t *testing.T) {
	for _, schema := range []string{`{"type":"integer","enum":[9007199254740993]}`, `{"type":"number","minimum":0.1234567890123456789}`} {
		r := req(t, `{"model":"fast","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"n":`+schema+`}}}}]}`)
		err := Check(nativeTarget("https://example.com/v1"), r)
		if err == nil || !strings.Contains(err.Error(), "unsupported_request") {
			t.Fatalf("lossy schema accepted: %s: %v", schema, err)
		}
	}
}

func TestNativeRequestAcceptsEquivalentSchemaNumbers(t *testing.T) {
	r := req(t, `{"model":"fast","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"n":{"type":"number","enum":[1.0,1e0,-0.0,1000e-3]}}}}}]}`)
	if err := Check(nativeTarget("https://example.com/v1"), r); err != nil {
		t.Fatal(err)
	}
}
