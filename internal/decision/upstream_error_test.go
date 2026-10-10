package decision

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJevUpstreamDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		status               int
		body, code, reported string
	}{
		{401, `{"error":{"code":"invalid_api_key","message":"SECRET"}}`, "upstream_authentication", "invalid_api_key"},
		{429, `{"error":{"code":"insufficient_quota","message":"SECRET"}}`, "upstream_quota", "insufficient_quota"},
		{429, `{"error":{"message":"SECRET"}}`, "upstream_rate_limit", ""},
		{503, `<html>SECRET</html>`, "upstream_unavailable", ""},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
		_, err := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: s.URL}, input())
		s.Close()
		d := routing.Diagnose(err)
		b, _ := json.Marshal(d)
		if d.Code != tc.code || d.UpstreamStatus != tc.status || d.UpstreamCode != tc.reported || strings.Contains(string(b), "SECRET") {
			t.Fatalf("%+v", d)
		}
	}
}

func TestJevErrorBodyTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(s.Client(), func(string) ([]byte, error) { return []byte("key"), nil }, DefaultBudget()).Choose(ctx, routing.DecisionConfig{BaseURL: s.URL}, input())
	if d := routing.Diagnose(err); d.Code != "timeout" || d.UpstreamCode != "" {
		t.Fatal(d)
	}
}
