package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaygroundCookieIdentity(t *testing.T) {
	sh, h := sessionHTTPFixture(t, "")
	token, info := sessionLogin(t, h, "")
	makeReq := func() *http.Request {
		r := httptest.NewRequest("POST", "http://localhost/admin/v1/playground/completions", strings.NewReader(`{}`))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", info.CSRFToken)
		r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
		return r
	}
	id, e := sh.AuthenticatePlayground(makeReq())
	if e != nil || id == "" || id == token || id == "settings" {
		t.Fatalf("identity %q %v", id, e)
	}
	id2, e := sh.AuthenticatePlayground(makeReq())
	if e != nil || id2 != id {
		t.Fatal("unstable identity")
	}
	for _, header := range []http.Header{{"Authorization": {"Bearer settings"}}, {"Origin": {"http://evil.test"}}, {"X-Csrf-Token": {"bad"}}, {"Cookie": {"jev_router_session=" + token + "; jev_router_session=" + token}}} {
		r := makeReq()
		for k, v := range header {
			r.Header[k] = v
		}
		if _, e = sh.AuthenticatePlayground(r); e == nil {
			t.Fatalf("accepted %v", header)
		}
	}
}
