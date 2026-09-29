package httptransport

import (
	"encoding/json"
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

func TestDiscoveryIncludesCompleteCallContract(t *testing.T) {
	s := management.New(nil, nil)
	h := NewManagementHandler(s, func(string) (management.Principal, error) { return management.Principal{Role: "settings"}, nil })
	for _, path := range []string{"/admin/v1/schema", "/admin/v1/schema/providers.put"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var result struct{ Data json.RawMessage }
		json.Unmarshal(w.Body.Bytes(), &result)
		var capabilities []map[string]json.RawMessage
		if path == "/admin/v1/schema" {
			json.Unmarshal(result.Data, &capabilities)
		} else {
			var c map[string]json.RawMessage
			json.Unmarshal(result.Data, &c)
			capabilities = append(capabilities, c)
		}
		for _, c := range capabilities {
			var id string
			json.Unmarshal(c["id"], &id)
			var call struct {
				Method, Path string
				Schema       struct {
					Properties map[string]json.RawMessage
					Required   []string
				}
				Protocol struct {
					Idempotency struct {
						ValidForHours            int  `json:"valid_for_hours"`
						ReplayBeforeVersionCheck bool `json:"replay_before_version_check"`
					}
					Errors map[string]struct {
						HTTP int `json:"http_status"`
						Exit int `json:"exit_code"`
					} `json:"error_mapping"`
				}
			}
			if json.Unmarshal(c["call"], &call) != nil || call.Method != "POST" || call.Path != "/admin/v1/call/"+id || len(call.Schema.Properties["input"]) == 0 || call.Protocol.Idempotency.ValidForHours != 24 || !call.Protocol.Idempotency.ReplayBeforeVersionCheck || call.Protocol.Errors["config_conflict"].HTTP != 409 || call.Protocol.Errors["config_conflict"].Exit != 5 {
				t.Fatalf("%s lacks shared call contract: %s", id, c["call"])
			}
			if id == "providers.put" {
				joined := strings.Join(call.Schema.Required, ",")
				if !strings.Contains(joined, "expected_version") || !strings.Contains(joined, "idempotency_key") {
					t.Fatal("write requirements missing")
				}
			}
		}
	}
}
