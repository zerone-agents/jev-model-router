package httptransport

import (
	"github.com/zerone-agents/jev-model-router/internal/management"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSchemaMatchesRegisteredHandlers(t *testing.T) {
	s := management.New(nil, nil)
	h := NewManagementHandler(s, func(string) (management.Principal, error) { return management.Principal{Role: "settings"}, nil })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/schema", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), `"id":"records.list"`) {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/schema/records.list", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestManagementRoleIsolation(t *testing.T) {
	h := NewManagementHandler(management.New(nil, nil), func(string) (management.Principal, error) { return management.Principal{Role: "inference"}, nil })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/v1/schema", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
