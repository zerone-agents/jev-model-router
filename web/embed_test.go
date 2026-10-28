package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardBoundary(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		code         int
	}{{"GET", "/", 308}, {"HEAD", "/", 308}, {"POST", "/", 405}, {"GET", "/dashboard/", 200}, {"HEAD", "/dashboard/", 200}, {"GET", "/dashboard", 308}, {"GET", "/dashboard/missing.js", 404}, {"GET", "/v1/models", 404}, {"POST", "/dashboard/", 405}} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if tc.code == 308 && w.Header().Get("Location") != "/dashboard/" {
			t.Fatalf("unexpected redirect: %q", w.Header().Get("Location"))
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing CSP")
		}
	}
}
