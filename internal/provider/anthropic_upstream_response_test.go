package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nativeJSON(t *testing.T, body string) (routing.Completion, error) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer s.Close()
	return generator(t).Complete(context.Background(), nativeTarget(s.URL), TestRequest())
}
func TestAnthropicUpstreamCompletion(t *testing.T) {
	for _, tc := range []struct {
		content, stop string
		ok            bool
	}{
		{`[{"type":"text","text":"OK"}]`, "end_turn", true},
		{`[{"type":"thinking","thinking":"reason","signature":"sig"},{"type":"text","text":"OK"}]`, "end_turn", true},
		{`[{"type":"mystery","text":"lost"}]`, "end_turn", false},
		{`[{"type":"text","text":"OK"}]`, "pause_turn", false},
		{`[{"type":"tool_use","id":"c","name":"f","input":{}},{"type":"tool_use","id":"c","name":"g","input":{}}]`, "tool_use", false},
		{`[{"type":"thinking","thinking":"reason","signature":"sig"},{"type":"tool_use","id":"c","name":"f","input":{}}]`, "tool_use", false},
	} {
		out, e := nativeJSON(t, `{"id":"m","type":"message","role":"assistant","model":"upstream","content":`+tc.content+`,"stop_reason":"`+tc.stop+`"}`)
		if (e == nil) != tc.ok {
			t.Errorf("%s: output %+v error %v", tc.content, out, e)
		}
	}
}
func TestAnthropicUpstreamUsage(t *testing.T) {
	out, e := nativeJSON(t, `{"id":"m","type":"message","role":"assistant","model":"upstream","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":7}}`)
	if e != nil {
		t.Fatal(e)
	}
	if out.Usage == nil || out.Usage.InputTokens != 60 || out.Usage.OutputTokens != 7 || out.Usage.TotalTokens != 67 {
		t.Fatalf("usage %+v", out.Usage)
	}
	out, e = nativeJSON(t, `{"id":"m","type":"message","role":"assistant","model":"upstream","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn"}`)
	if e != nil || out.Usage != nil {
		t.Fatalf("missing usage: %+v %v", out.Usage, e)
	}
}
func TestAnthropicUpstreamSafeErrors(t *testing.T) {
	for _, tc := range []struct{ status, want int }{{400, 400}, {401, 401}, {402, 402}, {403, 403}, {409, 409}, {404, 404}, {413, 413}, {422, 422}, {429, 429}, {529, 529}, {418, 418}, {503, 503}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"type":"error","error":{"type":"secret-type","message":"SECRET_SENTINEL","param":"secret-param"}}`)
			}))
			defer s.Close()
			for _, streaming := range []bool{false, true} {
				g := generator(t)
				r := TestRequest()
				r.Stream = streaming
				var e error
				if streaming {
					var stream routing.EventStream
					stream, e = g.Stream(context.Background(), nativeTarget(s.URL), r)
					if e == nil {
						_, e = stream.Next(context.Background())
						stream.Close()
					}
				} else {
					_, e = g.Complete(context.Background(), nativeTarget(s.URL), r)
				}
				var up *routing.UpstreamError
				if !errors.As(e, &up) || up.Status != tc.want {
					t.Fatalf("stream=%v error %#v", streaming, e)
				}
				if d := routing.Diagnose(e); d.UpstreamCode != "" {
					t.Fatalf("unrecognized code exposed: %+v", d)
				}
				b, _ := json.Marshal(up.Body)
				if strings.Contains(string(b), "secret") || strings.Contains(string(b), "SECRET") {
					t.Fatalf("unsafe error %s", b)
				}
			}
		})
	}
}

func TestNativeReportedErrorCodes(t *testing.T) {
	for _, code := range []string{"insufficient_quota", "content_policy_violation", "SECRET"} {
		e := nativeReportedError(400, []byte(`{"error":{"code":"`+code+`","message":"SECRET https://internal"}}`), "SECRET")
		d := routing.Diagnose(e)
		b, _ := json.Marshal(d)
		if strings.Contains(string(b), "SECRET") {
			t.Fatal(string(b))
		}
		if code != "SECRET" && d.UpstreamCode != code {
			t.Fatal(d)
		}
	}
}

func TestNativeDiagnosticCodeProvenance(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"status only", nativeError(429), ""},
		{"HTTP without code", nativeReportedError(429, `{"error":{"message":"limited"}}`), ""},
		{"HTTP unknown code", nativeReportedError(429, `{"error":{"code":"secret-unknown"}}`), ""},
		{"SSE rate limit", nativeEventError("rate_limit_error"), "rate_limit_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := routing.Diagnose(tc.err)
			if d.Code != "upstream_rate_limit" || d.UpstreamCode != tc.want {
				t.Fatalf("got %+v, want upstream_code %q", d, tc.want)
			}
			b, _ := json.Marshal(d)
			if tc.want == "" && strings.Contains(string(b), "upstream_code") {
				t.Fatal(string(b))
			}
		})
	}
}
