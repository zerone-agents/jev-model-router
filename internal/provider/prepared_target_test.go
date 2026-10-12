package provider

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
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
