package app

import (
	"context"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/decision"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	httptransport "github.com/zerone-agents/jev-model-router/internal/transport/http"
	"io"
	"net/http"
	"os"
	"time"
)

// Handler wires one instance. The returned close function releases its resources.
func Handler(cfg Config) (http.Handler, func(), error) {
	creds, e := LoadCredentials(cfg, os.LookupEnv, os.ReadFile)
	if e != nil {
		return nil, nil, e
	}
	store, e := state.Open(cfg.Database, time.Now)
	if e != nil {
		return nil, nil, e
	}
	resolve := func(ref string) ([]byte, error) { return ResolveSecret(ref, os.LookupEnv, os.ReadFile) }
	gen, e := provider.New(resolve)
	if e != nil {
		store.Close()
		return nil, nil, e
	}
	close := func() {
		if c, ok := gen.(io.Closer); ok {
			c.Close()
		}
		store.Close()
	}
	service := management.New(store, func(s routing.Snapshot) error {
		for _, p := range s.Providers {
			if _, e := resolve(p.SecretRef); e != nil {
				return routing.Fail("config_missing", "provider credential unavailable")
			}
		}
		if s.Decision.Model != "" {
			if _, e := resolve(s.Decision.SecretRef); e != nil {
				return routing.Fail("config_missing", "decision credential unavailable")
			}
		}
		return nil
	})
	planner := &routing.Planner{Decider: decision.New(nil, resolve, decision.DefaultBudget()), Check: provider.Check}
	checks := management.Checks{Store: store, Planner: planner, Generator: gen, DecisionTimeout: cfg.DecisionTimeout, FirstEventTimeout: cfg.FirstEventTimeout}
	checks.Register(service)
	auth := func(h string) (management.Principal, error) { return Authenticate(h, creds) }
	return httptransport.NewManagementHandler(service, auth), close, nil
}
func Run(ctx context.Context, cfg Config) error {
	h, close, e := Handler(cfg)
	if e != nil {
		return e
	}
	defer close()
	s := &http.Server{Addr: cfg.Listen, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if s.Shutdown(shutdown) != nil {
				s.Close()
			}
		case <-done:
		}
	}()
	e = s.ListenAndServe()
	closeChan(done)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return errors.New("server stopped unexpectedly")
}
func closeChan(ch chan struct{}) { close(ch) }
