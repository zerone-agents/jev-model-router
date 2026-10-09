package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func nativeTarget(url string) routing.Target {
	v := target(url)
	v.Provider.Protocol = routing.ProtocolAnthropic
	v.Model.UpstreamName = "claude-sonnet-4-5"
	return v
}
func TestAnthropicUpstreamTransport(t *testing.T) {
	for _, status := range []int{200, 401, 429, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected native request %s", r.URL.Path)
				}
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				if b["max_tokens"] != float64(16) {
					t.Errorf("output limit %v", b["max_tokens"])
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status != 200 {
					fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"sentinel"}}`)
					return
				}
				fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer s.Close()
			g := generator(t).(*bifrostGenerator)
			client, release, e := g.acquire(nativeTarget(s.URL + "/v1/"))
			if e != nil {
				t.Fatal(e)
			}
			defer release()
			bc := schemas.NewBifrostContext(context.Background(), time.Time{})
			defer bc.Cancel()
			request, e := encode(bc, nativeTarget(s.URL), TestRequest())
			if e != nil {
				t.Fatal(e)
			}
			request.Provider = schemas.Anthropic
			_, fail := client.ChatCompletionRequest(bc, request)
			if (fail == nil) != (status == 200) {
				t.Fatalf("status %d failure %v", status, fail)
			}
			if calls.Load() != 1 {
				t.Fatalf("sent %d requests", calls.Load())
			}
		})
	}
}
func TestProviderProtocolCacheIsolation(t *testing.T) {
	g := generator(t).(*bifrostGenerator)
	a := target("https://example.com/v1")
	first, r1, e := g.acquire(a)
	if e != nil {
		t.Fatal(e)
	}
	defer r1()
	a.Provider.Protocol = routing.ProtocolAnthropic
	second, r2, e := g.acquire(a)
	if e != nil {
		t.Fatal(e)
	}
	defer r2()
	if first == second {
		t.Fatal("protocols share client")
	}
	a.Provider.BaseURL += "/"
	third, r3, e := g.acquire(a)
	if e != nil {
		t.Fatal(e)
	}
	defer r3()
	if second != third {
		t.Fatal("trailing slash changes identity")
	}
}
