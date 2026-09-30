package httptransport

import (
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/session"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SessionHTTP struct {
	Service *session.Service
	origin  string
	bearer  func(string) (management.Principal, error)
}

func forbiddenSource() error {
	return routing.Fail("forbidden", "same-origin session request required")
}
func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// NormalizeDashboardOrigin accepts an origin only, never a proxy header or URL path.
func NormalizeDashboardOrigin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	bad := func() (string, error) {
		return "", routing.Fail("config_missing", "configure a valid HTTPS dashboard_origin (HTTP is allowed only on loopback)")
	}
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return bad()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "% /\\\t\r\n") || strings.HasSuffix(u.Host, ":") {
		return bad()
	}
	if u.Scheme == "http" && !loopback(host) {
		return bad()
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return bad()
		}
		port = strconv.Itoa(n)
	}
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return bad()
		}
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return u.Scheme + "://" + host, nil
}
func NewSessionHTTP(s *session.Service, origin string, auth func(string) (management.Principal, error)) (*SessionHTTP, error) {
	o, err := NormalizeDashboardOrigin(origin)
	if err != nil {
		return nil, err
	}
	return &SessionHTTP{Service: s, origin: o, bearer: auth}, nil
}
func headerPresent(r *http.Request, name string) bool {
	_, ok := r.Header[http.CanonicalHeaderKey(name)]
	return ok
}
func oneHeader(r *http.Request, name string) (string, bool) {
	v := r.Header.Values(name)
	return r.Header.Get(name), len(v) == 1 && v[0] != ""
}
func (s *SessionHTTP) source(r *http.Request) (string, error) {
	expected := s.origin
	if expected == "" {
		var err error
		expected, err = NormalizeDashboardOrigin("http://" + r.Host)
		if err != nil {
			return "", routing.Fail("config_missing", "configure dashboard_origin for remote browser login")
		}
	}
	u, _ := url.Parse(expected)
	requestOrigin, err := NormalizeDashboardOrigin(u.Scheme + "://" + r.Host)
	if err != nil || requestOrigin != expected {
		return "", forbiddenSource()
	}
	if headerPresent(r, "Sec-Fetch-Site") {
		v, ok := oneHeader(r, "Sec-Fetch-Site")
		if !ok || v != "same-origin" {
			return "", forbiddenSource()
		}
	}
	if r.Method != "GET" || headerPresent(r, "Origin") {
		value, ok := oneHeader(r, "Origin")
		if !ok {
			return "", forbiddenSource()
		}
		actual, err := NormalizeDashboardOrigin(value)
		if err != nil || actual != expected {
			return "", forbiddenSource()
		}
	}
	if r.Method == "GET" {
		v, ok := oneHeader(r, "X-Jev-Session")
		if !ok || v != "1" {
			return "", forbiddenSource()
		}
	} else {
		v, ok := oneHeader(r, "Content-Type")
		kind, _, err := mime.ParseMediaType(v)
		if !ok || err != nil || kind != "application/json" {
			return "", routing.Fail("invalid_request", "JSON body required")
		}
	}
	return expected, nil
}
func sessionCookie(r *http.Request) (string, error) {
	name := contracts.SessionLimits().Cookie.Name
	cookies := r.CookiesNamed(name)
	if len(cookies) > 1 {
		return "", routing.Fail("unauthorized", "ambiguous session cookie")
	}
	if len(cookies) == 0 {
		return "", nil
	}
	return cookies[0].Value, nil
}
func (s *SessionHTTP) bearerPrincipal(r *http.Request) (management.Principal, error) {
	value, ok := oneHeader(r, "Authorization")
	if !ok {
		return management.Principal{}, routing.Fail("unauthorized", "credential required")
	}
	return s.bearer(value)
}
func (s *SessionHTTP) Authenticate(r *http.Request) (management.Principal, error) {
	if headerPresent(r, "Authorization") {
		return s.bearerPrincipal(r)
	}
	origin, err := s.source(r)
	if err != nil {
		return management.Principal{}, err
	}
	token, err := sessionCookie(r)
	if err != nil {
		return management.Principal{}, err
	}
	if _, err = s.Service.Status(r.Context(), origin, token); err != nil {
		return management.Principal{}, err
	}
	if r.Method != "GET" {
		csrf, ok := oneHeader(r, "X-CSRF-Token")
		if !ok || !s.Service.CheckCSRF(token, csrf) {
			return management.Principal{}, routing.Fail("forbidden", "session changed; reconnect before retrying")
		}
	}
	return management.Principal{ID: "settings", Role: "settings"}, nil
}
func (s *SessionHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(err error) { writeResult(w, management.Failure(err)) }
	login := r.URL.Path == "/admin/v1/session/login" && r.Method == "POST"
	status := r.URL.Path == "/admin/v1/session" && r.Method == "GET"
	logout := r.URL.Path == "/admin/v1/session/logout" && r.Method == "POST"
	if !login && !status && !logout {
		fail(routing.Fail("not_found", "endpoint unavailable"))
		return
	}
	if login {
		p, err := s.bearerPrincipal(r)
		if err != nil {
			fail(err)
			return
		}
		if p.Role != "settings" {
			fail(routing.Fail("forbidden", "settings credential required"))
			return
		}
	} else if headerPresent(r, "Authorization") {
		fail(routing.Fail("unauthorized", "cookie authentication required"))
		return
	}
	origin, err := s.source(r)
	if err != nil {
		fail(err)
		return
	}
	token, err := sessionCookie(r)
	if err != nil {
		fail(err)
		return
	}
	if !status {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contracts.SessionLimits().BodyLimitBytes))
		id := "session.logout"
		if login {
			id = "session.login"
		}
		if err != nil || contracts.Validate(id, b) != nil {
			fail(routing.Fail("invalid_request", "empty JSON object required"))
			return
		}
	}
	if login {
		issued, err := s.Service.Login(r.Context(), origin, token)
		if err != nil {
			fail(err)
			return
		}
		policy := contracts.SessionLimits()
		http.SetCookie(w, &http.Cookie{Name: policy.Cookie.Name, Value: issued.Token, Path: policy.Cookie.Path, HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: policy.TTLSeconds, Expires: issued.ExpiresAt})
		writeResult(w, management.Success(issued.Info, time.Now()))
		return
	}
	if status {
		info, err := s.Service.Status(r.Context(), origin, token)
		if err != nil {
			fail(err)
			return
		}
		writeResult(w, management.Success(info, time.Now()))
		return
	}
	csrf, ok := oneHeader(r, "X-CSRF-Token")
	if !ok {
		fail(forbiddenSource())
		return
	}
	if err := s.Service.Logout(r.Context(), origin, token, csrf); err != nil {
		fail(err)
		return
	}
	writeResult(w, management.Success(map[string]bool{"revoked": true}, time.Now()))
}
func NewSessionManagementHandler(service *management.Service, s *SessionHTTP) http.Handler {
	admin := newRequestManagementHandler(service, s.Authenticate)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/admin/v1/session" || strings.HasPrefix(r.URL.Path, "/admin/v1/session/") {
			s.ServeHTTP(w, r)
			return
		}
		admin.ServeHTTP(w, r)
	})
}
