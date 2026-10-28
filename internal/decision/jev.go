package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net"
	"net/http"
	"net/url"
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
		// All production callers bound the complete decision with a context.
		// Do not impose DefaultTransport's separate ten-second TLS deadline.
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			cloned := transport.Clone()
			cloned.TLSHandshakeTimeout = 0
			client.Transport = cloned
		}
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
	path := decisionPath(c)
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
			Error json.RawMessage `json:"error"`
			Usage json.RawMessage `json:"usage"`
		}
		reported := ""
		if len(b) <= 65536 && json.Unmarshal(b, &envelope) == nil {
			zero.Usage = parseDecisionUsage(envelope.Usage, c)
			var upstreamError map[string]any
			if json.Unmarshal(envelope.Error, &upstreamError) == nil {
				reported = routing.ReportedErrorCode(upstreamError)
			}
		}
		details := routing.SanitizeUpstream(b, string(key))
		for _, header := range []string{"request-id", "x-request-id", "cf-ray"} {
			if id := resp.Header.Get(header); id != "" {
				details.SetRequestID(id, string(key))
				break
			}
		}
		return zero, &routing.UpstreamError{Status: resp.StatusCode, ReportedCode: reported, Details: details,
			Body: map[string]any{"code": "upstream_error", "type": "upstream_error", "message": "decision upstream rejected request", "param": nil}}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return zero, decisionIOError(ctx, e)
	}
	var out struct {
		Answers json.RawMessage
		Usage   json.RawMessage
	}
	if json.Unmarshal(b, &out) != nil {
		return zero, routing.Fail("invalid_decision", "invalid decision response")
	}
	usage := parseDecisionUsage(out.Usage, c)
	var answers map[string]struct{ Type, Choice string }
	if json.Unmarshal(out.Answers, &answers) != nil {
		return routing.Decision{Usage: usage}, routing.Fail("invalid_decision", "invalid decision response")
	}
	result := routing.Decision{Usage: usage}
	if in.SelectEffort {
		a := answers["effort"]
		valid := false
		for _, level := range []string{"minimal", "low", "medium", "high", "xhigh", "max"} {
			if a.Choice == level {
				valid = true
			}
		}
		if a.Type != "choice" || !valid {
			return result, routing.Fail("invalid_decision", "invalid effort response")
		}
		result.Effort = a.Choice
		if len(in.Candidates) == 1 {
			result.ModelID = in.Candidates[0].ID
			return result, nil
		}
	}
	a := answers["model"]
	if a.Type == "choice" {
		for n, m := range in.Candidates {
			if a.Choice == fmt.Sprintf("m%d", n) {
				result.ModelID = m.ID
				return result, nil
			}
		}
	}
	return result, routing.Fail("invalid_decision", "decision selected an unknown candidate")
}

func decisionIOError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return routing.Fail("timeout", "decision timed out")
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return routing.Fail("cancelled", "decision cancelled")
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return routing.Fail("timeout", "decision transport timed out")
	}
	return routing.Fail("upstream_error", "decision transport failed")
}

// A partial or malformed usage object must not become a known zero count.
func parseUsage(raw json.RawMessage) *routing.Usage {
	var counts struct {
		Input  *int64 `json:"input_tokens"`
		Output *int64 `json:"output_tokens"`
	}
	if json.Unmarshal(raw, &counts) != nil || counts.Input == nil || counts.Output == nil || *counts.Input < 0 || *counts.Output < 0 {
		return nil
	}
	var u routing.Usage
	if json.Unmarshal(raw, &u) != nil {
		return nil
	}
	return &u
}

// Aliyun System One reports input-only usage for decision-model-preview.
// Missing output usage is zero by this interface's accounting contract;
// explicit malformed output fields and other endpoints remain strict.
func parseDecisionUsage(raw json.RawMessage, c routing.DecisionConfig) *routing.Usage {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" ||
		!strings.HasSuffix(strings.ToLower(u.Hostname()), ".maas.aliyuncs.com") ||
		strings.TrimRight(u.Path, "/")+decisionPath(c) != "/compatible-mode/v1/systemone" ||
		c.Model != "decision-model-preview" {
		return parseUsage(raw)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil
	}
	if _, exists := fields["output_tokens"]; !exists {
		fields["output_tokens"] = json.RawMessage("0")
		normalized, err := json.Marshal(fields)
		if err != nil {
			return nil
		}
		return parseUsage(normalized)
	}
	return parseUsage(raw)
}

// decisionPath is shared by request construction and endpoint-specific parsing.
func decisionPath(c routing.DecisionConfig) string {
	if c.Path == "" {
		return "/v1/systemone"
	}
	return c.Path
}
