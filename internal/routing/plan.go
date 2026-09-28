package routing

import (
	"context"
	"encoding/json"
)

type Planner struct {
	Decider  Decider
	Check    func(Target, Request) error
	Estimate func(Model, Request) (int64, bool, error)
}
type Plan struct {
	ConfigVersion int64    `json:"config_version"`
	Target        Target   `json:"-"`
	ModelID       string   `json:"model_id"`
	CandidateIDs  []string `json:"candidate_ids"`
	Path          string   `json:"path"`
	DecisionUsage *Usage   `json:"decision_usage,omitempty"`
	ContextExact  bool     `json:"context_exact"`
}

// EstimateContext deliberately overcounts text using UTF-8 bytes and reserves
// 8192 units per image. Providers remain the authority on actual token limits.
func EstimateContext(_ Model, r Request) (int64, bool, error) {
	copy := r
	copy.Messages = append([]Message{}, r.Messages...)
	images := int64(0)
	for i, m := range copy.Messages {
		var parts []map[string]json.RawMessage
		if json.Unmarshal(m.Content, &parts) == nil && parts != nil {
			for _, p := range parts {
				if string(p["type"]) == `"image_url"` {
					images++
					delete(p, "image_url")
				}
			}
			copy.Messages[i].Content, _ = json.Marshal(parts)
		}
	}
	b, e := json.Marshal(copy)
	if e != nil {
		return 0, false, Fail("invalid_request", "cannot estimate context")
	}
	var output int64 = 4096
	if v := r.Options["max_completion_tokens"]; v != nil {
		json.Unmarshal(v, &output)
	}
	return int64(len(b)) + images*8192 + output + 256, false, nil
}
func (p *Planner) Plan(ctx context.Context, s Snapshot, r Request) (Plan, error) {
	result := Plan{ConfigVersion: s.Version, CandidateIDs: []string{}}
	if e := ValidateRequest(r); e != nil {
		return result, e
	}
	// Freeze all slices before any external decision call.
	s.Models = append([]Model{}, s.Models...)
	s.Providers = append([]Provider{}, s.Providers...)
	if r.Model == "auto" {
		enabled := false
		for _, m := range s.Models {
			if m.Enabled {
				enabled = true
				break
			}
		}
		if !enabled {
			return result, Fail("config_missing", "configure and enable a generation model")
		}
	}

	estimate := p.Estimate
	if estimate == nil {
		estimate = EstimateContext
	}
	targets := []Target{}
	exact := map[string]bool{}
	for _, m := range s.Models {
		if r.Model != "auto" && m.ID != r.Model {
			continue
		}
		if !m.Enabled {
			continue
		}
		var provider Provider
		found := false
		for _, v := range s.Providers {
			if v.ID == m.ProviderID {
				provider = v
				found = true
				break
			}
		}
		if !found {
			return result, Fail("config_missing", "model provider missing")
		}
		target := Target{Provider: provider, Model: m}
		var e error
		if HasImages(r) && !m.Capabilities.Images || len(r.Tools) > 0 && !m.Capabilities.Tools || RequiresStructuredOutput(r) && !m.Capabilities.StructuredOutput {
			e = Fail("unsupported_request", "model capabilities do not support request")
		}
		if e == nil && p.Check != nil {
			e = p.Check(target, r)
		}
		if e != nil {
			if r.Model != "auto" {
				return result, e
			}
			continue
		}
		units, isExact, e := estimate(m, r)
		if e != nil {
			return result, e
		}
		if units > m.Capabilities.ContextLimit {
			if r.Model != "auto" {
				return result, Fail("budget_exceeded", "generation context exceeds configured capacity")
			}
			continue
		}
		targets = append(targets, target)
		exact[m.ID] = isExact
		result.CandidateIDs = append(result.CandidateIDs, m.ID)
	}
	if len(targets) == 0 {
		if r.Model != "auto" {
			return result, Fail("not_found", "model unavailable")
		}
		return result, Fail("no_candidates", "no enabled model can accept the complete request")
	}
	selected := targets[0]
	if r.Model != "auto" {
		result.Path = "explicit"
	} else if len(targets) == 1 {
		result.Path = "single_candidate"
	} else {
		result.Path = "jev_choice"
		if p.Decider == nil || s.Decision.Model == "" {
			return result, Fail("config_missing", "decision configuration required")
		}
		models := make([]Model, len(targets))
		for i, v := range targets {
			models[i] = v.Model
		}
		d, e := p.Decider.Choose(ctx, s.Decision, DecisionInput{Prompt: s.Prompt, Candidates: models, Request: r})
		if e != nil {
			return result, e
		}
		found := false
		for _, v := range targets {
			if v.Model.ID == d.ModelID {
				selected = v
				found = true
				break
			}
		}
		if !found {
			return result, Fail("invalid_decision", "unknown selected model")
		}
		result.DecisionUsage = d.Usage
	}
	result.Target = selected
	result.ModelID = selected.Model.ID
	result.ContextExact = exact[selected.Model.ID]
	return result, nil
}
