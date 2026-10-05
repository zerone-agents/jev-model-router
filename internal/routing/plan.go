package routing

import (
	"context"
)

type Planner struct {
	Decider  Decider
	Check    func(Target, Request) error
	Estimate func(Model, Request) (int64, bool, error)
}
type Plan struct {
	ConfigVersion   int64            `json:"config_version"`
	Target          Target           `json:"-"`
	ModelID         string           `json:"model_id"`
	CandidateIDs    []string         `json:"candidate_ids"`
	Path            string           `json:"path"`
	DecisionUsage   *Usage           `json:"decision_usage,omitempty"`
	ContextEstimate *ContextEstimate `json:"context_estimate,omitempty"`
	ContextExact    bool             `json:"context_exact"`
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
	var breakdown *ContextEstimate
	if r.Model == "auto" && estimate == nil {
		value, err := EstimateRequestContext(r)
		if err != nil {
			return result, err
		}
		breakdown = &value
		estimate = func(Model, Request) (int64, bool, error) { return value.TotalTokens, false, nil }
	}
	targets := []Target{}
	var overflow []Target
	exactOverflow := false
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
		if HasImages(r) && !m.Capabilities.Images || RequiresTools(r) && !m.Capabilities.Tools || RequiresStructuredOutput(r) && !m.Capabilities.StructuredOutput {
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
		// Explicit selection delegates context capacity entirely to the provider.
		if r.Model != "auto" {
			result.Target = target
			result.ModelID = m.ID
			result.CandidateIDs = []string{m.ID}
			result.Path = "explicit"
			return result, nil
		}
		units, isExact, e := estimate(m, r)
		if e != nil {
			return result, e
		}
		if units > m.Capabilities.ContextLimit {
			if isExact {
				exactOverflow = true
			} else {
				if len(overflow) == 0 || m.Capabilities.ContextLimit > overflow[0].Model.Capabilities.ContextLimit {
					overflow = []Target{target}
				} else if m.Capabilities.ContextLimit == overflow[0].Model.Capabilities.ContextLimit {
					overflow = append(overflow, target)
				}
			}
			continue
		}
		targets = append(targets, target)
		exact[m.ID] = isExact
		result.CandidateIDs = append(result.CandidateIDs, m.ID)
	}
	fallback := len(targets) == 0 && len(overflow) > 0 && !exactOverflow
	if fallback {
		targets = overflow
		for _, target := range targets {
			result.CandidateIDs = append(result.CandidateIDs, target.Model.ID)
		}
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
	} else if fallback {
		result.Path = "context_estimate_fallback"
	} else if len(targets) == 1 {
		result.Path = "single_candidate"
	} else {
		result.Path = "jev_choice"
	}
	if r.Model == "auto" && len(targets) > 1 {
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
	result.ContextEstimate = breakdown
	return result, nil
}
