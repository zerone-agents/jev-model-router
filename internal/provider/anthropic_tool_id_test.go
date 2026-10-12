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

func TestMessagesToolIDContinuation(t *testing.T) {
	for _, tc := range []struct {
		name, first, next string
		fails             bool
	}{
		{"absent", `"id":"call1",`, "", false},
		{"null", `"id":"call1",`, `"id":null,`, false},
		{"empty", `"id":"call1",`, `"id":"",`, false},
		{"same", `"id":"call1",`, `"id":"call1",`, false},
		{"changed", `"id":"call1",`, `"id":"call2",`, true},
		{"missing_initial", "", "", true},
		{"empty_initial", `"id":"",`, `"id":"",`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,%s\"function\":{\"name\":\"lookup\",\"arguments\":\"\"}}]}}]}\n\n", tc.first)
				fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,%s\"function\":{\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", tc.next)
			}))
			defer s.Close()
			p, err := PrepareMessages(json.RawMessage(`{"model":"external","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"query"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			stream, err := generator(t).(*bifrostGenerator).StreamMessages(context.Background(), target(s.URL), p)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var output string
			var arguments string
			for {
				event, e := stream.Next(context.Background())
				if e == io.EOF {
					break
				}
				if e != nil {
					err = e
					break
				}
				output += string(event.Data)
				if event.Type == "content_block_delta" {
					var d struct {
						Delta struct {
							PartialJSON string `json:"partial_json"`
						} `json:"delta"`
					}
					if json.Unmarshal(event.Data, &d) != nil {
						t.Fatal("invalid event")
					}
					arguments += d.Delta.PartialJSON
				}
			}
			if (err != nil) != tc.fails {
				t.Fatalf("fails=%t error=%v", tc.fails, err)
			}
			if tc.fails && strings.Contains(output, `message_stop`) {
				t.Fatal("invalid tool completed")
			}
			if !tc.fails && arguments != "{}" {
				t.Fatalf("lost arguments: %q", arguments)
			}
			if !tc.fails && (!strings.Contains(output, `"id":"call1"`) || !strings.Contains(output, `message_stop`)) {
				t.Fatalf("incomplete stream: %s", output)
			}
		})
	}
}
