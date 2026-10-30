package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// TestLayaDecisionFixtures captures production request construction without a live provider.
func TestLayaDecisionFixtures(t *testing.T) {
	fixtures := map[string]json.RawMessage{}
	for _, n := range []int{0, 1, 2, 5, 20, 40} {
		in := input()
		in.SelectEffort = n != 0
		modelOnly := n == 0
		if modelOnly {
			n = 2
		}
		in.Candidates = nil
		for i := 0; i < n; i++ {
			in.Candidates = append(in.Candidates, routing.Model{ID: fmt.Sprintf("model-%d", i), Description: []string{"Fast everyday text tasks.", "Complex code and mathematical reasoning."}[i%2]})
		}
		name := fmt.Sprintf("models-%d", n)
		if modelOnly {
			name = "model-only"
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			fixtures[name] = body
			var req struct {
				Questions map[string]struct{ Criteria map[string]string }
			}
			if err = json.Unmarshal(body, &req); err != nil {
				t.Error(err)
				return
			}
			if !modelOnly && len(req.Questions["effort"].Criteria) != 6 {
				t.Error("lost effort options")
			}
			if !modelOnly {
				previous := -1
				for _, level := range []string{"minimal", "low", "medium", "high", "xhigh", "max"} {
					position := strings.Index(string(body), `"`+level+`":`)
					if position <= previous {
						t.Errorf("effort order lost: %s", level)
					}
					previous = position
				}
			}
			for i, model := range in.Candidates {
				if n > 1 && !strings.Contains(req.Questions["model"].Criteria[fmt.Sprintf("m%d", i)], model.Description) {
					t.Errorf("description lost for %s", model.ID)
				}
			}
			if n > 1 && len(req.Questions["model"].Criteria) != n {
				t.Error("lost model options")
			}
			_, _ = w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"},"effort":{"type":"choice","choice":"low"}}}`))
		}))
		_, err := New(srv.Client(), func(string) ([]byte, error) { return []byte("fixture"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: srv.URL, Model: "laya-multilingual"}, in)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if path := os.Getenv("LAYA_FIXTURE_OUTPUT"); path != "" {
		b, err := json.MarshalIndent(fixtures, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
