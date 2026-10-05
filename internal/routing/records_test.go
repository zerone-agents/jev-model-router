package routing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type failedSink struct{}

func (failedSink) Append(context.Context, Record) error { return errors.New("SECRET") }
func TestRecorderDegraded(t *testing.T) {
	r := Recorder{Sink: failedSink{}, Timeout: time.Millisecond}
	r.Save(Record{})
	if !r.Degraded() {
		t.Fatal("silent failure")
	}
}

func TestRequestSummary(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"latest user", `{"messages":[{"role":"user","content":"old"},{"role":"user","content":"  新请求\n hello\tworld  "},{"role":"tool","content":"secret tool output"}]}`, "新请求 hello world"},
		{"text parts", `{"messages":[{"role":"system","content":"secret"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://secret"}},{"type":"text","text":"one "},{"type":"text","text":" two"}]}]}`, "one two"},
		{"no text", `{"messages":[{"role":"user","content":"old"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://secret"}}]}]}`, ""},
		{"no user", `{"messages":[{"role":"assistant","content":"secret"}]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req Request
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatal(err)
			}
			if got := RequestSummary(req); got != tc.want {
				t.Fatalf("%q != %q", got, tc.want)
			}
		})
	}
	content, _ := json.Marshal(strings.Repeat("中文😀", 60))
	got := RequestSummary(Request{Messages: []Message{{Role: "user", Content: content}}})
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 120 || got != strings.Repeat("中文😀", 40) {
		t.Fatalf("bad truncation: %q", got)
	}
}
