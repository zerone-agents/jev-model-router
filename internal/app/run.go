package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/decision"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/session"
	"github.com/zerone-agents/jev-model-router/internal/state"
	httptransport "github.com/zerone-agents/jev-model-router/internal/transport/http"
	"github.com/zerone-agents/jev-model-router/web"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Handler wires one instance. The returned close function releases its resources.
func Handler(cfg Config) (http.Handler, func(), error) {
	origin, e := httptransport.NormalizeDashboardOrigin(cfg.DashboardOrigin)
	if e != nil {
		return nil, nil, e
	}
	creds, e := LoadCredentials(cfg, os.LookupEnv, os.ReadFile)
	if e != nil {
		return nil, nil, e
	}
	cipher, e := loadEncryptionCipher(cfg.EncryptionKeyRef)
	if e != nil {
		return nil, nil, e
	}
	store, e := state.OpenWithCipher(cfg.Database, time.Now, cipher)
	if e != nil {
		return nil, nil, e
	}
	if e = store.ConfigureRecordLimit(context.Background(), cfg.RecordMaxCount); e != nil {
		store.Close()
		return nil, nil, e
	}
	sessions, e := session.New(context.Background(), store, creds.settings, origin, time.Now)
	if e != nil {
		store.Close()
		return nil, nil, e
	}
	externalResolve := func(ref string) ([]byte, error) { return ResolveSecret(ref, os.LookupEnv, os.ReadFile) }
	resolve := func(ref string) ([]byte, error) {
		if strings.HasPrefix(ref, "managed:") {
			return store.ResolveManaged(ref)
		}
		return externalResolve(ref)
	}
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
			// State validates managed revisions using its active transaction.
			if strings.HasPrefix(p.SecretRef, "managed:") {
				continue
			}
			if _, e := externalResolve(p.SecretRef); e != nil {
				return routing.Fail("config_missing", "provider credential unavailable")
			}
		}
		if s.Decision.Model != "" {
			if _, e := externalResolve(s.Decision.SecretRef); e != nil {
				return routing.Fail("config_missing", "decision credential unavailable")
			}
		}
		return nil
	})
	service.ResolveSecret = resolve
	budget := decision.DefaultBudget()
	budget.MaxBytes = cfg.DecisionMaxBytes
	planner := &routing.Planner{Decider: decision.New(nil, externalResolve, budget), Check: provider.Check}
	checks := management.Checks{Store: store, Planner: planner, Generator: gen, DecisionTimeout: cfg.DecisionTimeout, FirstEventTimeout: cfg.FirstEventTimeout}
	checks.Register(service)
	service.RecordsDegraded = recorder.Degraded
	service.Register("records.list", func(ctx context.Context, _ string, call management.Call) (management.Result, error) {
		var in struct {
			Cursor string
			Limit  int
			Offset int
		}
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return management.Result{}, routing.Fail("invalid_request", "invalid record pagination")
		}
		page, e := store.ListRecords(ctx, in.Cursor, in.Limit, in.Offset)
		if e != nil {
			return management.Result{}, e
		}
		return management.Success(page, time.Now()), nil
	})
	auth := func(h string) (management.Principal, error) { return Authenticate(h, creds) }
	api := httptransport.NewHandler(service, store, planner, &routing.Executor{Generator: gen}, auth, httptransport.Limits{Recorder: recorder, DecisionTimeout: cfg.DecisionTimeout, FirstEventTimeout: cfg.FirstEventTimeout, IdleTimeout: cfg.IdleTimeout, MaxBodyBytes: cfg.MaxBodyBytes, StreamBuffer: cfg.StreamBuffer})
	sessionHTTP, e := httptransport.NewSessionHTTP(sessions, origin, auth)
	if e != nil {
		close()
		return nil, nil, e
	}
	admin := httptransport.NewSessionManagementHandler(service, sessionHTTP)
	dashboard := web.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard" || strings.HasPrefix(r.URL.Path, "/dashboard/") {
			dashboard.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			admin.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	}), close, nil
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
