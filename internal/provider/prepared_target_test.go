package provider

import (
	"context"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreparedTargetCredentialRotation(t *testing.T) {
	value := "credential-A"
	reads := 0
	raw, e := New(func(string) ([]byte, error) { reads++; return []byte(value), nil })
	if e != nil {
		t.Fatal(e)
	}
	g := raw.(*bifrostGenerator)
	defer g.Close()
	target := routing.Target{Provider: routing.Provider{ID: "p", BaseURL: "https://example.com", SecretRef: "file:/key"}, Model: routing.Model{ID: "m", Revision: "r"}}
	prepare := g.Preparer([32]byte{1})
	a, e := prepare(context.Background(), target)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Prepared.Close()
	value = "credential-B"
	_, release, secret, e := g.acquire(a)
	if e != nil {
		t.Fatal(e)
	}
	release()
	if secret != "credential-A" || reads != 1 {
		t.Fatalf("snapshot reread: %s %d", secret, reads)
	}
	b, e := prepare(context.Background(), target)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Prepared.Close()
	if a.Prepared.Identity() == b.Prepared.Identity() {
		t.Fatal("rotation did not change identity")
	}
	_, release, secret, e = g.acquire(a)
	if e != nil {
		t.Fatal(e)
	}
	release()
	if secret != "credential-A" || reads != 2 {
		t.Fatal("cache hit replaced actual credential")
	}
}

func TestHealthClassifiesActualNetworkFailure(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := up.URL
	up.Close()
	raw, _ := New(func(string) ([]byte, error) { return []byte("key"), nil })
	g := raw.(*bifrostGenerator)
	defer g.Close()
	target := routing.Target{Provider: routing.Provider{ID: "p", BaseURL: url + "/v1"}, Model: routing.Model{ID: "m", UpstreamName: "up"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e := g.Complete(ctx, target, TestRequest())
	if routing.HealthOutcome(e).Kind != health.Transient {
		t.Fatalf("network failure not counted: %T %v", e, e)
	}
}
func TestHealthEmptyErrorBodyPreservesStatus(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(status)
			}))
			defer up.Close()
			raw, _ := New(func(string) ([]byte, error) { return []byte("key"), nil })
			g := raw.(*bifrostGenerator)
			defer g.Close()
			_, e := g.Complete(context.Background(), routing.Target{Provider: routing.Provider{BaseURL: up.URL + "/v1"}, Model: routing.Model{ID: "m", UpstreamName: "m"}}, TestRequest())
			outcome := routing.HealthOutcome(e)
			if outcome.Kind == "" || outcome.Status != status {
				t.Fatalf("lost HTTP status: %+v %v", outcome, e)
			}
			if status == 429 && outcome.RetryAt.Before(time.Now().Add(59*time.Minute)) {
				t.Fatalf("lost retry header: %+v", outcome)
			}
		})
	}
}
func TestHealthReportedPermanentCodes(t *testing.T) {
	for _, code := range []string{"model_not_found", "insufficient_balance", "quota_exceeded", "billing_error", "context_length_exceeded", "rate_limit_error"} {
		t.Run(code, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(429)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":"test"}}`, code)
			}))
			defer up.Close()
			raw, _ := New(func(string) ([]byte, error) { return []byte("key"), nil })
			g := raw.(*bifrostGenerator)
			defer g.Close()
			_, e := g.Complete(context.Background(), routing.Target{Provider: routing.Provider{BaseURL: up.URL + "/v1"}, Model: routing.Model{ID: "m", UpstreamName: "m"}}, TestRequest())
			want := health.Permanent
			if code == "context_length_exceeded" {
				want = ""
			}
			if code == "rate_limit_error" {
				want = health.Limited
			}
			if o := routing.HealthOutcome(e); o.Kind != want {
				t.Fatalf("%s mapped to %+v", code, o)
			}
		})
	}
}
func TestHealthMessagesMalformedSuccess(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"c","choices":[]}`))
	}))
	defer up.Close()
	raw, _ := New(func(string) ([]byte, error) { return []byte("key"), nil })
	g := raw.(*bifrostGenerator)
	defer g.Close()
	p, e := PrepareMessages([]byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.CompleteMessages(context.Background(), routing.Target{Provider: routing.Provider{BaseURL: up.URL + "/v1"}, Model: routing.Model{ID: "m", UpstreamName: "m"}}, p)
	if routing.HealthOutcome(e).Kind != health.Transient {
		t.Fatalf("malformed Messages response ignored: %v", e)
	}
}
func TestPreparedFileCredentialUsedOnWire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(path, []byte("test-A"), 0600); e != nil {
		t.Fatal(e)
	}
	auth := make(chan string, 2)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
	}))
	defer up.Close()
	raw, _ := New(func(string) ([]byte, error) { return os.ReadFile(path) })
	g := raw.(*bifrostGenerator)
	defer g.Close()
	prepare := g.Preparer([32]byte{1})
	target := routing.Target{Provider: routing.Provider{BaseURL: up.URL + "/v1"}, Model: routing.Model{ID: "m", UpstreamName: "m"}}
	a, e := prepare(context.Background(), target)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Prepared.Close()
	if e = os.WriteFile(path, []byte("test-B"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Complete(context.Background(), a, TestRequest()); e != nil {
		t.Fatal(e)
	}
	if got := <-auth; got != "Bearer test-A" {
		t.Fatal("in-flight call changed credentials")
	}
	b, e := prepare(context.Background(), target)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Prepared.Close()
	if _, e = g.Complete(context.Background(), b, TestRequest()); e != nil {
		t.Fatal(e)
	}
	if got := <-auth; got != "Bearer test-B" || a.HealthIdentity() == b.HealthIdentity() {
		t.Fatal("new call did not adopt new credential")
	}
}
func TestHealthNativeRetryAfter(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"wait"}}`))
	}))
	defer up.Close()
	raw, _ := New(func(string) ([]byte, error) { return []byte("key"), nil })
	g := raw.(*bifrostGenerator)
	defer g.Close()
	_, e := g.Complete(context.Background(), routing.Target{Provider: routing.Provider{BaseURL: up.URL + "/v1", Protocol: routing.ProtocolAnthropic}, Model: routing.Model{ID: "m", UpstreamName: "m"}}, TestRequest())
	o := routing.HealthOutcome(e)
	if o.Kind != health.Limited || o.RetryAt.Before(time.Now().Add(59*time.Minute)) {
		t.Fatalf("native retry lost: %+v %v", o, e)
	}
}
