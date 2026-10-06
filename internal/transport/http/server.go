package httptransport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"strings"
	"time"
)

type Limits struct {
	Recorder                                        *routing.Recorder
	DecisionTimeout, FirstEventTimeout, IdleTimeout time.Duration
	MaxBodyBytes                                    int64
	StreamBuffer                                    int
}

func (l Limits) defaults() Limits {
	if l.DecisionTimeout <= 0 {
		l.DecisionTimeout = 10 * time.Second
	}
	if l.FirstEventTimeout <= 0 {
		l.FirstEventTimeout = 60 * time.Second
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = 60 * time.Second
	}
	if l.MaxBodyBytes <= 0 {
		l.MaxBodyBytes = 16 << 20
	}
	if l.StreamBuffer <= 0 {
		l.StreamBuffer = 8
	}
	return l
}

type server struct {
	store     management.ConfigStore
	planner   *routing.Planner
	executor  *routing.Executor
	limits    Limits
	inference *inference.Service
}

func NewHandler(service *management.Service, store management.ConfigStore, planner *routing.Planner, executor *routing.Executor, auth func(string) (management.Principal, error), limits Limits) http.Handler {
	admin := NewManagementHandler(service, auth)
	limits = limits.defaults()
	s := server{store: store, planner: planner, executor: executor, limits: limits, inference: &inference.Service{Store: store, Planner: planner, Recorder: limits.Recorder, DecisionTimeout: limits.DecisionTimeout}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			admin.ServeHTTP(w, r)
			return
		}
		w.Header().Set("X-Request-ID", rand.Text())
		if r.URL.Path == "/v1/messages" {
			w.Header().Set("request-id", w.Header().Get("X-Request-ID"))
			if r.Method != "POST" {
				writeMessagesError(w, routing.Fail("method_not_allowed", "POST required"), w.Header().Get("X-Request-ID"))
				return
			}
			p, e := messagesPrincipal(r, auth)
			if e != nil {
				writeMessagesError(w, e, w.Header().Get("X-Request-ID"))
				return
			}
			if p.Role != "inference" {
				writeMessagesError(w, routing.Fail("forbidden", "inference credential required"), w.Header().Get("X-Request-ID"))
				return
			}
			s.messages(w, r)
			return
		}
		p, e := auth(r.Header.Get("Authorization"))
		if e != nil {
			writeError(w, e)
			return
		}
		if p.Role != "inference" {
			writeError(w, routing.Fail("forbidden", "inference credential required"))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/v1/models" {
			s.models(w, r)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
			s.chat(w, r)
			return
		}
		writeError(w, routing.Fail("not_found", "endpoint unavailable"))
	})
}
func (s *server) models(w http.ResponseWriter, r *http.Request) {
	cfg, e := s.store.Snapshot(r.Context())
	if e != nil {
		writeError(w, e)
		return
	}
	items := []map[string]any{{"id": "auto", "object": "model", "created": 0, "owned_by": "router"}}
	for _, m := range cfg.Models {
		if m.Enabled {
			items = append(items, map[string]any{"id": m.ID, "object": "model", "created": 0, "owned_by": "router"})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": items})
}
func contextError(ctx context.Context, e error) error {
	if ctx.Err() == context.DeadlineExceeded || context.Cause(ctx) == context.DeadlineExceeded {
		return routing.Fail("timeout", "request timed out")
	}
	if ctx.Err() != nil {
		return routing.Fail("cancelled", "request cancelled")
	}
	// Bifrost watches the inherited deadline with a separate timer. Its failure
	// can arrive just before the parent context publishes DeadlineExceeded.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return routing.Fail("timeout", "request timed out")
	}
	return e
}
