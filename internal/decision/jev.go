package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"strings"
)

type jev struct {
	client  *http.Client
	resolve func(string) ([]byte, error)
	budget  BudgetPolicy
}

func New(client *http.Client, resolve func(string) ([]byte, error), budget BudgetPolicy) routing.Decider {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &jev{&copy, resolve, normalize(budget)}
}
func (j *jev) Choose(ctx context.Context, c routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	var zero routing.Decision
	state, e := BuildState(in, j.budget)
	if e != nil {
		return zero, e
	}
	body, e := json.Marshal(map[string]any{"model": c.Model, "state": json.RawMessage(state), "questions": question(in)})
	if e != nil {
		return zero, routing.Fail("invalid_request", "cannot encode decision")
	}
	if len(body) > j.budget.MaxBytes {
		return zero, routing.Fail("budget_exceeded", "decision request exceeds byte budget")
	}
	key, e := j.resolve(c.SecretRef)
	if e != nil {
		return zero, routing.Fail("config_missing", "decision credential unavailable")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/systemone", bytes.NewReader(body))
	if e != nil {
		return zero, routing.Fail("config_missing", "invalid decision endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+string(key))
	req.Header.Set("Content-Type", "application/json")
	resp, e := j.client.Do(req)
	if e != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return zero, routing.Fail("timeout", "decision timed out")
		}
		if ctx.Err() != nil {
			return zero, routing.Fail("cancelled", "decision cancelled")
		}
		return zero, routing.Fail("upstream_error", "decision transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, routing.Fail("upstream_error", "decision upstream rejected request")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return zero, routing.Fail("upstream_error", "decision response interrupted")
	}
	var out struct {
		Answers map[string]struct{ Type, Choice string }
		Usage   *routing.Usage
	}
	if json.Unmarshal(b, &out) != nil {
		return zero, routing.Fail("invalid_decision", "invalid decision response")
	}
	a := out.Answers["model"]
	if a.Type == "choice" {
		for n, m := range in.Candidates {
			if a.Choice == fmt.Sprintf("m%d", n) {
				return routing.Decision{ModelID: m.ID, Usage: out.Usage}, nil
			}
		}
	}
	return zero, routing.Fail("invalid_decision", "decision selected an unknown candidate")
}
