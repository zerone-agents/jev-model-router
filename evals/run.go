// Command evals runs explicitly authorized, potentially paid evaluation calls.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type evalCase struct {
	ID         string         `json:"case_id"`
	Request    map[string]any `json:"request"`
	Acceptable []string       `json:"acceptable_model_ids"`
	Criteria   string         `json:"quality_criteria"`
}
type usage struct {
	Input  int64 `json:"input_tokens"`
	Output int64 `json:"output_tokens"`
}
type price struct{ Input, Output float64 }
type prices struct {
	Decision *price            `json:"decision"`
	Models   map[string]*price `json:"models"`
}
type metric struct {
	CaseID           string `json:"case_id"`
	ModelID          string `json:"model_id,omitempty"`
	Acceptable       bool   `json:"acceptable_selection"`
	Quality          string `json:"quality"`
	Outcome          string `json:"outcome"`
	DecisionMillis   int64  `json:"decision_ms"`
	GenerationMillis int64  `json:"generation_ms"`
	DecisionUsage    *usage `json:"decision_usage"`
	GenerationUsage  *usage `json:"generation_usage"`
	DecisionCost     any    `json:"decision_cost"`
	GenerationCost   any    `json:"generation_cost"`
}

func cost(u *usage, p *price) any {
	if u == nil || p == nil {
		return "unknown"
	}
	return (float64(u.Input)*p.Input + float64(u.Output)*p.Output) / 1e6
}
func post(ctx context.Context, client *http.Client, base, path, token string, input any, out any) error {
	b, e := json.Marshal(input)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+path, bytes.NewReader(b))
	if e != nil {
		return errors.New("invalid URL")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(r)
	if e != nil {
		return errors.New("request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("request rejected")
	}
	b, e = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if e != nil || json.Unmarshal(b, out) != nil {
		return errors.New("invalid response")
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	cases := flag.String("cases", "evals/cases.jsonl", "synthetic dataset")
	mode := flag.String("mode", "auto", "auto or fixed")
	model := flag.String("model", "", "fixed external model ID")
	output := flag.String("output", "", "new metrics JSONL file")
	priceFile := flag.String("prices", "", "optional per-million-token prices JSON")
	paid := flag.Bool("allow-paid", false, "authorize potentially paid calls")
	flag.Parse()
	if !*paid {
		return errors.New("explicit --allow-paid authorization required")
	}
	if (*mode != "auto" && *mode != "fixed") || (*mode == "fixed" && *model == "") || *output == "" {
		return errors.New("provide valid mode, model if fixed, and output")
	}
	base := os.Getenv("JEV_ROUTER_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	inference := os.Getenv("JEV_ROUTER_INFERENCE_TOKEN")
	settings := os.Getenv("JEV_ROUTER_SETTINGS_TOKEN")
	if inference == "" || (*mode == "auto" && settings == "") {
		return errors.New("required role credentials missing")
	}
	var pricing prices
	if *priceFile != "" {
		b, e := os.ReadFile(*priceFile)
		if e != nil || json.Unmarshal(b, &pricing) != nil {
			return errors.New("invalid prices file")
		}
	}
	f, e := os.Open(*cases)
	if e != nil {
		return errors.New("cases unavailable")
	}
	defer f.Close()
	list := []evalCase{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	ids := map[string]bool{}
	for scanner.Scan() {
		var c evalCase
		if json.Unmarshal(scanner.Bytes(), &c) != nil || c.ID == "" || ids[c.ID] || c.Request == nil || len(c.Acceptable) == 0 || c.Criteria == "" {
			return errors.New("invalid or duplicate evaluation case")
		}
		ids[c.ID] = true
		list = append(list, c)
	}
	if scanner.Err() != nil || len(list) == 0 {
		return errors.New("cannot read cases")
	}
	out, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("output must be a new writable file")
	}
	defer out.Close()
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	enc := json.NewEncoder(out)
	for _, c := range list {
		m := metric{CaseID: c.ID, ModelID: *model, Quality: "unknown", Outcome: "error", DecisionCost: "unknown", GenerationCost: "unknown"}
		if *mode == "auto" {
			c.Request["model"] = "auto"
			var result struct {
				OK   bool
				Data struct {
					ModelID string `json:"model_id"`
					Usage   *usage `json:"decision_usage"`
				}
			}
			start := time.Now()
			e = post(context.Background(), client, base, "/admin/v1/call/route.inspect", settings, map[string]any{"input": c.Request}, &result)
			m.DecisionMillis = time.Since(start).Milliseconds()
			if e != nil || !result.OK {
				if e = enc.Encode(m); e != nil {
					return e
				}
				continue
			}
			m.ModelID = result.Data.ModelID
			m.DecisionUsage = result.Data.Usage
			m.DecisionCost = cost(m.DecisionUsage, pricing.Decision)
		} else {
			m.DecisionCost = 0
		}
		c.Request["model"] = m.ModelID
		c.Request["stream"] = false
		delete(c.Request, "stream_options")
		var result struct {
			Usage *struct {
				Prompt     int64 `json:"prompt_tokens"`
				Completion int64 `json:"completion_tokens"`
			}
		}
		start := time.Now()
		e = post(context.Background(), client, base, "/v1/chat/completions", inference, c.Request, &result)
		m.GenerationMillis = time.Since(start).Milliseconds()
		if e == nil {
			m.Outcome = "success"
			if result.Usage != nil {
				m.GenerationUsage = &usage{result.Usage.Prompt, result.Usage.Completion}
			}
			for _, id := range c.Acceptable {
				if id == m.ModelID {
					m.Acceptable = true
				}
			}
		}
		m.GenerationCost = cost(m.GenerationUsage, pricing.Models[m.ModelID])
		if e = enc.Encode(m); e != nil {
			return e
		}
	}
	return nil
}
