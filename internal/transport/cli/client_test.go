package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func secretCall() management.Call {
	return management.Call{Input: json.RawMessage(`{"id":"p","base_url":"https://upstream.example/v1","api_key":"transport-secret-marker"}`)}
}
func TestSecretTransportPolicy(t *testing.T) {
	for _, tc := range []struct {
		base string
		ok   bool
	}{{"https://router.example", true}, {"http://router.example", false}, {"http://192.0.2.1", false}, {"http://localhost:1234", true}, {"http://127.0.0.1:1234", true}, {"http://[::1]:1234", true}, {"http://localhost.example", false}, {"http://127.0.0.1.example", false}} {
		u, _ := url.Parse(tc.base)
		if e := validateSecretTransport(u, secretCall()); (e == nil) != tc.ok {
			t.Errorf("%s: allowed=%v", tc.base, e == nil)
		}
		if e := validateSecretTransport(u, management.Call{Input: json.RawMessage(`{"id":"p"}`)}); e != nil {
			t.Fatal("nonsecret call changed")
		}
	}
}
func TestSecretHTTPRejectedBeforeSending(t *testing.T) {
	_, e := request(context.Background(), "http://192.0.2.1:1", "POST", "/admin/v1/call/providers.put", []byte("token"), secretCall())
	var re *routing.Error
	if !errors.As(e, &re) || re.Code != "invalid_request" || strings.Contains(e.Error(), "transport-secret-marker") {
		t.Fatal("not rejected before transport", e)
	}
}
func TestSecretRedirectNeverForwarded(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	_, e := request(context.Background(), source.URL, "POST", "/admin/v1/call/providers.put", []byte("token"), secretCall())
	if e == nil || calls != 0 || strings.Contains(e.Error(), "transport-secret-marker") {
		t.Fatal("redirect followed or leaked")
	}
}
