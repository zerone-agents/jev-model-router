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
	}{{"GET", "/dashboard/", 200}, {"HEAD", "/dashboard/", 200}, {"GET", "/dashboard", 308}, {"GET", "/dashboard/missing.js", 404}, {"GET", "/v1/models", 404}, {"POST", "/dashboard/", 405}} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing CSP")
		}
	}
}
