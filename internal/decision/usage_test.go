package decision

import (
	"context"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUsageValidityAndFailurePreservation(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"input_tokens":2}`, `{"input_tokens":-1,"output_tokens":2}`, `{"input_tokens":2,"output_tokens":0}`} {
		for _, status := range []int{200, 400} {
			t.Run(fmt.Sprintf("%s/%d", raw, status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"answers":{"model":{"type":"choice","choice":"invalid"}},"usage":%s}`, raw)
				}))
				defer server.Close()
				d, err := New(server.Client(), func(string) ([]byte, error) { return []byte("test"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: server.URL}, input())
				if err == nil {
					t.Fatal("invalid choice accepted")
				}
				valid := raw == `{"input_tokens":2,"output_tokens":0}`
				if (d.Usage != nil) != valid {
					t.Fatalf("usage=%+v", d.Usage)
				}
			})
		}
	}
}

func TestUsageSurvivesMalformedResponseFields(t *testing.T) {
	for _, status := range []int{200, 400} {
		for _, field := range []string{`"wrong"`, `[]`, `42`, `{"model":{"type":7,"choice":false}}`} {
			for _, usageFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%s/usage-first=%t", status, field, usageFirst), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(status)
						fields := fmt.Sprintf(`"answers":%s,"error":%s`, field, field)
						usage := `"usage":{"input_tokens":12,"output_tokens":3}`
						if usageFirst {
							fmt.Fprintf(w, `{%s,%s}`, usage, fields)
						} else {
							fmt.Fprintf(w, `{%s,%s}`, fields, usage)
						}
					}))
					defer server.Close()
					d, err := New(server.Client(), func(string) ([]byte, error) { return []byte("test"), nil }, DefaultBudget()).Choose(context.Background(), routing.DecisionConfig{BaseURL: server.URL}, input())
					if err == nil {
						t.Fatal("malformed response accepted")
					}
					if d.Usage == nil || d.Usage.InputTokens != 12 || d.Usage.OutputTokens != 3 {
						t.Fatalf("reported usage lost: %+v", d.Usage)
					}
					if status == 200 && routing.Diagnose(err).Code != "invalid_decision" {
						t.Fatalf("wrong error: %v", err)
					}
				})
			}
		}
	}
}
