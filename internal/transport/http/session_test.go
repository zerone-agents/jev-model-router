package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/session"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func sessionHTTPFixture(t *testing.T, origin string) (*SessionHTTP, http.Handler) {
	t.Helper()
	st, err := state.Open(filepath.Join(t.TempDir(), "state.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := session.New(context.Background(), st, sha256.Sum256([]byte("settings")), origin, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth := func(h string) (management.Principal, error) {
		if h == "Bearer settings" {
			return management.Principal{ID: "settings", Role: "settings"}, nil
		}
		if h == "Bearer inference" {
			return management.Principal{ID: "inference", Role: "inference"}, nil
		}
		return management.Principal{}, routing.Fail("unauthorized", "invalid credential")
	}
	sh, err := NewSessionHTTP(s, origin, auth)
	if err != nil {
		t.Fatal(err)
	}
	return sh, NewSessionManagementHandler(management.New(st, nil), sh)
}
func sessionRequest(h http.Handler, method, path, body, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("X-Jev-Session", "1")
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if path == "/admin/v1/session/login" {
		r.Header.Set("Authorization", "Bearer settings")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func sessionLogin(t *testing.T, h http.Handler, old string) (string, session.Info) {
	t.Helper()
	w := sessionRequest(h, "POST", "/admin/v1/session/login", "{}", old, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("cookie missing")
	}
	c := cookies[0]
	if !c.HttpOnly || c.Path != "/admin/" || c.MaxAge != 86400 || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || c.Secure {
		t.Fatal(c)
	}
	var result struct{ Data session.Info }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), c.Value) {
		t.Fatal("leaked token")
	}
	return c.Value, result.Data
}
func TestSessionHTTPBoundaries(t *testing.T) {
	sh, h := sessionHTTPFixture(t, "")
	a, info := sessionLogin(t, h, "")
	for _, tc := range []struct {
		method, path, body, csrf string
		code                     int
	}{{"GET", "/admin/v1/session", "", "", 200}, {"GET", "/admin/v1/schema", "", "", 200}, {"POST", "/admin/v1/call/status.get", `{"input":{}}`, info.CSRFToken, 200}, {"POST", "/admin/v1/call/status.get", `{"input":{}}`, "wrong", 403}, {"POST", "/admin/v1/session/logout", "{}", "wrong", 403}} {
		w := sessionRequest(h, tc.method, tc.path, tc.body, a, tc.csrf)
		if w.Code != tc.code {
			t.Fatalf("%s %d %s", tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Header())
		}
	}
	for _, header := range []http.Header{{"Authorization": {""}}, {"Authorization": {"Bearer bad"}}, {"Authorization": {"Bearer settings", "Bearer settings"}}, {"Origin": {"null"}}, {"Origin": {"http://evil.localhost"}}, {"Origin": {"http://localhost", "http://localhost"}}, {"Sec-Fetch-Site": {"same-site"}}, {"X-Jev-Session": {""}}, {"Cookie": {"jev_router_session=" + a + "; jev_router_session=" + a}}} {
		r := httptest.NewRequest("GET", "http://localhost/admin/v1/schema", nil)
		r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: a})
		r.Header.Set("X-Jev-Session", "1")
		for k, v := range header {
			r.Header[k] = v
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("accepted invalid headers", header)
		}
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatal("cookie mutated")
		}
	}
	// Explicit CLI Bearer bypasses browser restrictions.
	r := httptest.NewRequest("GET", "http://remote/admin/v1/schema", nil)
	r.Header.Set("Authorization", "Bearer settings")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	b, newInfo := sessionLogin(t, h, a)
	if w := sessionRequest(h, "GET", "/admin/v1/session", "", a, ""); w.Code != 401 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("old status")
	}
	if w := sessionRequest(h, "POST", "/admin/v1/session/logout", "{}", a, info.CSRFToken); w.Code != 401 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("old logout")
	}
	if w := sessionRequest(h, "POST", "/admin/v1/session/logout", "{}", b, info.CSRFToken); w.Code != 403 {
		t.Fatal("csrf mismatch")
	}
	if w := sessionRequest(h, "GET", "/admin/v1/session", "", b, ""); w.Code != 200 {
		t.Fatal("S1 revoked")
	}
	if w := sessionRequest(h, "POST", "/admin/v1/session/logout", "{}", b, newInfo.CSRFToken); w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("logout")
	}
	if _, err := sh.Service.Status(context.Background(), "http://localhost", b); err == nil {
		t.Fatal("logout not revoked")
	}
}
func TestSessionLoginValidation(t *testing.T) {
	_, h := sessionHTTPFixture(t, "")
	for _, body := range []string{`null`, `{"x":1}`, `{} {}`, strings.Repeat(" ", 1025) + "{}"} {
		if w := sessionRequest(h, "POST", "/admin/v1/session/login", body, "", ""); w.Code != 400 || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("accepted body", w.Code)
		}
	}
	for _, headers := range []http.Header{{"Authorization": {"Bearer inference"}}, {"Authorization": {"Bearer settings", "Bearer settings"}}, {"Origin": {""}}, {"Content-Type": {"text/plain"}}, {"Sec-Fetch-Site": {"cross-site"}}} {
		r := httptest.NewRequest("POST", "http://localhost/admin/v1/session/login", strings.NewReader("{}"))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Authorization", "Bearer settings")
		r.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			r.Header[k] = v
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || w.Header().Get("Set-Cookie") != "" {
			t.Fatal(headers, w.Code)
		}
	}
}
func TestSessionOriginPolicy(t *testing.T) {
	for _, bad := range []string{"http://example.com", "https://example.com/path", "https://user@example.com", "https://example.com?x", "https://example.com#x", "https://example.com:", "null"} {
		if _, err := NormalizeDashboardOrigin(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	for _, good := range []string{"https://EXAMPLE.com:443/", "http://[::1]:80", "http://127.0.0.2:8000"} {
		if _, err := NormalizeDashboardOrigin(good); err != nil {
			t.Fatal(good, err)
		}
	}
	_, h := sessionHTTPFixture(t, "https://router.example")
	r := httptest.NewRequest("POST", "http://router.example/admin/v1/session/login", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://router.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer settings")
	r.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !w.Result().Cookies()[0].Secure {
		t.Fatal("proxy secure", w.Code, w.Body.String())
	}
	_, h = sessionHTTPFixture(t, "")
	r.Host = "remote.example"
	r.Header.Set("X-Forwarded-Host", "localhost")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("unconfigured remote", w.Code)
	}
}
