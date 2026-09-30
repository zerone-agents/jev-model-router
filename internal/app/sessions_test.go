package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardSessionRestartAndRoles(t *testing.T) {
	t.Setenv("SESSION_SETTINGS", "settings-A")
	t.Setenv("SESSION_INFERENCE", "inference-A")
	cfg := Config{Listen: "127.0.0.1:8080", Database: filepath.Join(t.TempDir(), "state.db"), SettingsRef: "env:SESSION_SETTINGS", InferenceRef: "env:SESSION_INFERENCE", RetentionDays: 7}
	h, close, err := Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := func(h http.Handler, method, path, auth string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader("{}"))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Jev-Session", "1")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request(h, "POST", "/admin/v1/session/login", "settings-A", nil)
	if w.Code != 200 {
		close()
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if request(h, "GET", "/v1/models", "", cookie).Code != 401 {
		t.Fatal("cookie accessed inference")
	}
	if request(h, "GET", "/v1/models", "inference-A", nil).Code != 200 {
		t.Fatal("inference bearer")
	}
	close()
	t.Setenv("SESSION_INFERENCE", "inference-B")
	h, close, err = Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if request(h, "GET", "/admin/v1/session", "", cookie).Code != 200 {
		t.Fatal("inference rotation revoked session")
	}
	close()
	t.Setenv("SESSION_SETTINGS", "settings-B")
	h, close, err = Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if request(h, "GET", "/admin/v1/session", "", cookie).Code != 401 {
		t.Fatal("settings rotation retained session")
	}
	close()
	t.Setenv("SESSION_SETTINGS", "settings-A")
	h, close, err = Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if request(h, "GET", "/admin/v1/session", "", cookie).Code != 401 {
		t.Fatal("A-B-A revived session")
	}
}
func TestDashboardOriginConfig(t *testing.T) {
	for _, value := range []string{"https://EXAMPLE.com:443/", "http://localhost:8080", ""} {
		c, err := LoadConfig("", func(k string) (string, bool) { return value, k == "JEV_ROUTER_DASHBOARD_ORIGIN" })
		if err != nil {
			t.Fatal(value, err)
		}
		if value != "" && c.DashboardOrigin == "" {
			t.Fatal("missing origin")
		}
	}
	for _, value := range []string{"http://remote.example", "https://example.com/path"} {
		if _, err := LoadConfig("", func(k string) (string, bool) { return value, k == "JEV_ROUTER_DASHBOARD_ORIGIN" }); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
}
