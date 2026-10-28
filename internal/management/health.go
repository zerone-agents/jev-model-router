package management

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type modelView struct {
	routing.Model
	AutomaticEffortLevels []string     `json:"automatic_effort_levels"`
	Health                *health.View `json:"health,omitempty"`
}

func (s *Service) modelView(ctx context.Context, cfg routing.Snapshot, m routing.Model) (any, bool) {
	view := modelView{Model: m, AutomaticEffortLevels: modelEffortLevels(cfg, m)}
	if s.Health == nil {
		return view, false
	}
	for _, p := range cfg.Providers {
		if p.ID == m.ProviderID {
			t, e := s.Health.PrepareTarget(ctx, routing.Target{Model: m, Provider: p})
			if e != nil {
				return view, true
			}
			if t.Prepared != nil {
				defer t.Prepared.Close()
			}
			r, e := s.Health.Service.Get(t.HealthIdentity())
			if e != nil {
				return view, true
			}
			if !m.Enabled {
				r.NextProbe = nil
			}
			view.Health = &r.View
			return view, s.Health.Service.Warning()
		}
	}
	return view, true
}

func modelEffortLevels(cfg routing.Snapshot, m routing.Model) []string {
	for _, p := range cfg.Providers {
		if p.ID == m.ProviderID {
			return routing.AutomaticEffortLevels(m, p.EffectiveProtocol())
		}
	}
	return []string{}
}
