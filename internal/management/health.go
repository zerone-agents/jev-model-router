package management

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type modelView struct {
	routing.Model
	Health *health.View `json:"health,omitempty"`
}

func (s *Service) modelView(ctx context.Context, cfg routing.Snapshot, m routing.Model) (any, bool) {
	if s.Health == nil {
		return m, false
	}
	for _, p := range cfg.Providers {
		if p.ID == m.ProviderID {
			t, e := s.Health.PrepareTarget(ctx, routing.Target{Model: m, Provider: p})
			if e != nil {
				return modelView{Model: m}, true
			}
			if t.Prepared != nil {
				defer t.Prepared.Close()
			}
			r, e := s.Health.Service.Get(t.HealthIdentity())
			if e != nil {
				return modelView{Model: m}, true
			}
			if !m.Enabled {
				r.NextProbe = nil
			}
			return modelView{Model: m, Health: &r.View}, s.Health.Service.Warning()
		}
	}
	return modelView{Model: m}, true
}
