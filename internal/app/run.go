package app

import (
	"context"
	"encoding/json"
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
	recordCtx, recordCancel := context.WithCancel(context.Background())
	recordDone := make(chan struct{})
	recorder := &routing.Recorder{Sink: store, Timeout: cfg.RecordTimeout}
	prune := func() {
		ctx, cancel := context.WithTimeout(recordCtx, cfg.RecordTimeout)
		defer cancel()
		if store.Prune(ctx, time.Now(), cfg.RetentionDays) != nil {
			recorder.MarkDegraded()
		}
	}
	prune()
	go func() {
		defer close(recordDone)
		tick := time.NewTicker(time.Hour)
		defer tick.Stop()
		for {
			select {
			case <-recordCtx.Done():
				return
			case <-tick.C:
				prune()
			}
		}
	}()
	close := func() {
		recordCancel()
		<-recordDone
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
	service.RecordsDegraded = recorder.Degraded
	service.Register("records.list", func(ctx context.Context, _ string, call management.Call) (management.Result, error) {
		var in struct {
			Cursor string
			Limit  int
		}
		json.Unmarshal(call.Input, &in)
		page, e := store.ListRecords(ctx, in.Cursor, in.Limit)
		if e != nil {
			return management.Result{}, e
		}
		return management.Success(page, time.Now()), nil
	})
	auth := func(h string) (management.Principal, error) { return Authenticate(h, creds) }
	return httptransport.NewHandler(service, store, planner, &routing.Executor{Generator: gen}, auth, httptransport.Limits{Recorder: recorder, DecisionTimeout: cfg.DecisionTimeout, FirstEventTimeout: cfg.FirstEventTimeout, IdleTimeout: cfg.IdleTimeout, MaxBodyBytes: cfg.MaxBodyBytes, StreamBuffer: cfg.StreamBuffer}), close, nil
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
