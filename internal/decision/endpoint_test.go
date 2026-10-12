package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func TestConfiguredDecisionPath(t *testing.T) {
	for _, path := range []string{"", "/api/v1/decisions"} {
		t.Run(path, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := path
				if want == "" {
					want = "/v1/systemone"
				}
				if r.URL.Path != want || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("unexpected endpoint/auth")
				}
				var body struct {
					Model     string
					Questions map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != "typesafe/jev-1.13" || body.Questions["model"] == nil {
					t.Error("request lost configuration or question")
				}
				w.Write([]byte(`{"model":"typesafe/jev-1.13-20260917","answers":{"model":{"type":"choice","choice":"m0"}},"usage":{"input_tokens":327,"output_tokens":34,"cost":0.000027468},"task_id":"test","consumed":"0.000027468"}`))
			}))
			defer server.Close()
			got, err := New(server.Client(), func(string) ([]byte, error) { return []byte("test-key"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: server.URL + "/", Path: path, Model: "typesafe/jev-1.13"}, input())
			if err != nil || got.ModelID != "fast" || got.Usage == nil || got.Usage.InputTokens != 327 {
				t.Fatalf("decision=%+v err=%v", got, err)
			}
		})
	}
}
