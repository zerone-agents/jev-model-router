package provider

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"sync"
)

type preparedCredential struct {
	mu     sync.Mutex
	owner  *bifrostGenerator
	key    []byte
	id     string
	closed bool
}

func (p *preparedCredential) Identity() string { return p.id }
func (p *preparedCredential) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.key)
	p.key = nil
	p.closed = true
	return nil
}
func (g *bifrostGenerator) Preparer(key [32]byte) func(context.Context, routing.Target) (routing.Target, error) {
	return func(ctx context.Context, t routing.Target) (routing.Target, error) {
		if ctx.Err() != nil {
			return t, ctx.Err()
		}
		secret, e := g.resolve(t.Provider.SecretRef)
		if e != nil {
			return t, routing.Fail("config_missing", "provider credential unavailable")
		}
		revision := health.Fingerprint(key, secret)
		if strings.HasPrefix(t.Provider.SecretRef, "managed:") {
			revision = t.Provider.SecretRef
		}
		id := health.Identity(key, t.Model.Revision, t.Model.ID, t.Provider.ID, string(t.Provider.EffectiveProtocol()), t.Provider.BaseURL, t.Model.UpstreamName, revision)
		t.Prepared = &preparedCredential{owner: g, key: append([]byte(nil), secret...), id: string(id)}
		return t, nil
	}
}
func (g *bifrostGenerator) credential(t routing.Target) ([]byte, error) {
	if t.Prepared == nil {
		return g.resolve(t.Provider.SecretRef)
	}
	p, ok := t.Prepared.(*preparedCredential)
	if !ok || p.owner != g {
		return nil, routing.Fail("internal_error", "invalid prepared target")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, routing.Fail("internal_error", "prepared target closed")
	}
	return append([]byte(nil), p.key...), nil
}
