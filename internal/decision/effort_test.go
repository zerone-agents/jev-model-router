package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndependentEffortQuestions(t *testing.T) {
	for _, sole := range []bool{false, true} {
		in := input()
		in.SelectEffort = true
		if sole {
			in.Candidates = in.Candidates[:1]
		}
		for _, level := range []string{"low", "high"} {
			in.Candidates[0].Capabilities.Reasoning = append(in.Candidates[0].Capabilities.Reasoning, routing.ReasoningCombination{Effort: level})
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b struct {
				State     json.RawMessage
				Questions map[string]json.RawMessage
			}
			json.NewDecoder(r.Body).Decode(&b)
			var effort struct{ Criteria json.RawMessage }
			if err := json.Unmarshal(b.Questions["effort"], &effort); err != nil {
				t.Error(err)
				return
			}
			previous := -1
			for _, level := range []string{"minimal", "low", "medium", "high", "xhigh", "max"} {
				pos := bytes.Index(effort.Criteria, []byte(`"`+level+`":`))
				if pos <= previous {
					t.Errorf("criteria not in strength order: %s", effort.Criteria)
				}
				previous = pos
			}
			want := 2
			if sole {
				want = 1
			}
			if len(b.Questions) != want || b.Questions["effort"] == nil {
				t.Error("wrong questions", string(b.Questions["effort"]))
			}
			if bytes.Contains(b.State, []byte("auto_effort")) {
				t.Error("mapping leaked")
			}
			w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"},"effort":{"type":"choice","choice":"xhigh"}}}`))
		}))
		d, err := New(srv.Client(), func(string) ([]byte, error) { return []byte("k"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: srv.URL}, in)
		srv.Close()
		if err != nil || d.ModelID != "fast" || d.Effort != "xhigh" {
			t.Fatalf("%+v %v", d, err)
		}
	}
}

func TestEffortFortyModelShapeAndBudget(t *testing.T) {
	in := input()
	in.SelectEffort = true
	in.Candidates = nil
	for i := 0; i < 40; i++ {
		in.Candidates = append(in.Candidates, routing.Model{ID: fmt.Sprintf("m%d", i), Description: "Useful model", Capabilities: routing.Capabilities{Reasoning: []routing.ReasoningCombination{{Effort: "low"}, {Effort: "medium"}, {Effort: "high"}, {Effort: "xhigh"}, {Effort: "max"}}}})
	}
	q, _ := json.Marshal(question(in))
	var questions map[string]struct{ Criteria map[string]any }
	json.Unmarshal(q, &questions)
	if len(questions) != 2 || len(questions["model"].Criteria) != 40 || len(questions["effort"].Criteria) != 6 {
		t.Fatal("cartesian expansion or lost levels")
	}
	body, err := BuildState(in, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("auto_effort")) {
		t.Fatal("mapping in task state")
	}
	budget := DefaultBudget()
	budget.MaxBytes = len(q)
	if _, err = BuildState(in, budget); err == nil {
		t.Fatal("questions bypassed byte budget")
	}
}
func TestMalformedEffortAnswer(t *testing.T) {
	for _, answer := range []string{`{}`, `{"effort":{"type":"choice","choice":"none"}}`, `{"effort":{"type":"text","choice":"high"}}`} {
		in := input()
		in.SelectEffort = true
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"answers":%s}`, answer) }))
		_, err := New(srv.Client(), func(string) ([]byte, error) { return []byte("k"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: srv.URL}, in)
		srv.Close()
		if err == nil {
			t.Fatal("malformed accepted", answer)
		}
	}
}
