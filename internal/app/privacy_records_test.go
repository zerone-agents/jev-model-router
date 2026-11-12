package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

func TestPrivacyRecordManagementFilter(t *testing.T) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "inference-test")
	cfg, err := LoadConfig("", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = filepath.Join(t.TempDir(), "db")
	db, err := state.Open(cfg.Database, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []routing.Record{
		{RequestID: "classified", CreatedAt: time.Now(), PrivacyOutcome: routing.PrivacyNonSensitive},
		{RequestID: "ordinary", CreatedAt: time.Now()},
	} {
		if err := db.Append(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	h, cleanup, err := Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, tc := range []struct {
		input string
		total int
		id    string
	}{
		{`{"privacy_only":true,"limit":1}`, 1, "classified"},
		{`{"privacy_only":false,"limit":1}`, 2, "ordinary"},
		{`{"limit":1}`, 2, "ordinary"},
	} {
		req := httptest.NewRequest("POST", "/admin/v1/call/records.list", strings.NewReader(`{"input":`+tc.input+`}`))
		req.Header.Set("Authorization", "Bearer settings-test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var response struct{ Data state.RecordPage }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || response.Data.Total != tc.total || len(response.Data.Records) != 1 || response.Data.Records[0].RequestID != tc.id {
			t.Fatalf("%s: %d %s", tc.input, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/admin/v1/schema/records.list", nil)
	req.Header.Set("Authorization", "Bearer settings-test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"privacy_only"`) {
		t.Fatal("missing discovery", w.Body.String())
	}
}
