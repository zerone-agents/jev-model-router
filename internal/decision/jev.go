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
	path := c.Path
	if path == "" {
		path = "/v1/systemone"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if e != nil {
		return zero, routing.Fail("config_missing", "invalid decision endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+string(key))
	req.Header.Set("Content-Type", "application/json")
	resp, e := j.client.Do(req)
	if e != nil {
		return zero, decisionIOError(ctx, e)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 65537))
		if readErr != nil {
			return zero, decisionIOError(ctx, readErr)
		}
		var envelope struct {
			Error map[string]any `json:"error"`
		}
		reported := ""
		if len(b) <= 65536 && json.Unmarshal(b, &envelope) == nil {
			reported = routing.ReportedErrorCode(envelope.Error)
		}
		return zero, &routing.UpstreamError{Status: resp.StatusCode, ReportedCode: reported,
			Body: map[string]any{"code": "upstream_error", "type": "upstream_error", "message": "decision upstream rejected request", "param": nil}}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return zero, decisionIOError(ctx, e)
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

func decisionIOError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return routing.Fail("timeout", "decision timed out")
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return routing.Fail("cancelled", "decision cancelled")
	}
	return routing.Fail("upstream_error", "decision transport failed")
}
