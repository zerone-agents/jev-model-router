package management

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"sort"
	"strings"
	"sync"
	"time"
)

type ConfigStore interface {
	Snapshot(context.Context) (routing.Snapshot, error)
	Apply(context.Context, string, Call, func(routing.Snapshot) error) (Result, error)
}
type Handler func(context.Context, string, Call) (Result, error)
type Service struct {
	store           ConfigStore
	validate        func(routing.Snapshot) error
	mu              sync.RWMutex
	handlers        map[string]Handler
	RecordsDegraded func() bool
	ResolveSecret   func(string) ([]byte, error)
}

func New(store ConfigStore, validate func(routing.Snapshot) error) *Service {
	return &Service{store: store, validate: validate, handlers: map[string]Handler{}}
}
func Success(data any, now time.Time) Result {
	b, _ := json.Marshal(data)
	return Result{OK: true, Data: b, Warnings: []string{}, Meta: Meta{OperationID: rand.Text(), Timestamp: now.UTC().Format(time.RFC3339Nano), SchemaVersion: "1"}}
}
func Failure(err error) Result {
	var known *routing.Error
	if !errors.As(err, &known) {
		known = &routing.Error{Code: "internal_error", Message: "operation failed"}
	}
	r := Success(nil, time.Now())
	r.OK = false
	r.Data = nil
	r.Error = known
	return r
}
func (s *Service) Register(id string, h func(context.Context, string, Call) (Result, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[id] = h
}
func (s *Service) Catalog() []contracts.Capability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []contracts.Capability
	for _, c := range contracts.Catalog() {
		if s.handlers[c.ID] != nil || builtin(c.ID) {
			c.Availability = "available"
			out = append(out, c)
		}
	}
	return out
}
func builtin(id string) bool {
	return id == "status.get" || strings.HasPrefix(id, "providers.") || strings.HasPrefix(id, "models.") && id != "models.test" || strings.HasPrefix(id, "decision.") || strings.HasPrefix(id, "prompt.")
}
func (s *Service) Execute(ctx context.Context, principal string, c Call) (Result, error) {
	s.mu.RLock()
	h := s.handlers[c.CapabilityID]
	s.mu.RUnlock()
	if h != nil {
		if contracts.Validate(c.CapabilityID, c.Input) != nil {
			return Result{}, routing.Fail("invalid_request", "invalid capability input")
		}
		return h(ctx, principal, c)
	}
	if !builtin(c.CapabilityID) {
		return Result{}, routing.Fail("not_found", "capability unavailable")
	}
	if strings.HasSuffix(c.CapabilityID, ".put") || strings.HasSuffix(c.CapabilityID, ".delete") {
		return s.store.Apply(ctx, principal, c, s.validate)
	}
	if contracts.Validate(c.CapabilityID, c.Input) != nil {
		return Result{}, routing.Fail("invalid_request", "invalid capability input")
	}
	cfg, e := s.store.Snapshot(ctx)
	if e != nil {
		return Result{}, e
	}
	var input struct {
		ID       string `json:"id"`
		Cursor   string `json:"cursor"`
		Limit    int    `json:"limit"`
		Sort     string `json:"sort"`
		Offset   *int   `json:"offset"`
		Query    string `json:"query"`
		Language string `json:"language"`
	}
	json.Unmarshal(c.Input, &input)
	if input.Offset != nil && input.Cursor != "" {
		return Result{}, routing.Fail("invalid_request", "offset and cursor cannot be combined")
	}
	var resource any
	switch c.CapabilityID {
	case "status.get":
		n := 0
		for _, m := range cfg.Models {
			if m.Enabled {
				n++
			}
		}
		degraded := s.RecordsDegraded != nil && s.RecordsDegraded()
		result := Success(map[string]any{"version": cfg.Version, "ready": n > 0 && (n == 1 || cfg.Decision.Model != ""), "enabled_models": n, "records_degraded": degraded}, time.Now())
		if degraded {
			result.Warnings = append(result.Warnings, "records_degraded")
		}
		return result, nil
	case "prompt.get":
		return Success(map[string]any{"version": cfg.Version, "text": cfg.Prompt}, time.Now()), nil
	case "decision.get":
		resource = cfg.Decision
	case "providers.get":
		for _, p := range cfg.Providers {
			if p.ID == input.ID {
				var masked any
				if s.ResolveSecret != nil {
					if key, err := s.ResolveSecret(p.SecretRef); err == nil && len(key) > 0 {
						prefix := []rune(string(key))
						// Keep short credentials fully masked.
						if len(prefix) > 8 {
							masked = string(prefix[:4]) + "***"
						} else {
							masked = "***"
						}
					}
				}
				result := Success(map[string]any{"version": cfg.Version, "resource": p, "api_key_masked": masked}, time.Now())
				if masked == nil {
					result.Warnings = append(result.Warnings, "provider_credential_unavailable")
				}
				return result, nil
			}
		}
	case "models.get":
		for _, m := range cfg.Models {
			if m.ID == input.ID {
				resource = m
			}
		}
	case "providers.list", "models.list":
		items := []any{}
		type item struct {
			id string
			v  any
		}
		all := []item{}
		if c.CapabilityID == "providers.list" {
			for _, p := range cfg.Providers {
				all = append(all, item{p.ID, p})
			}
		} else {
			for _, m := range cfg.Models {
				if !modelMatches(m, input.Query, input.Language) {
					continue
				}
				key := m.ID
				if input.Sort == "enabled_first" {
					prefix := "1:"
					if m.Enabled {
						prefix = "0:"
					}
					key = prefix + m.ID
				}
				all = append(all, item{key, m})
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
		limit := input.Limit
		if limit == 0 {
			limit = 50
		}
		next := ""
		last := ""
		for index, x := range all {
			if input.Offset != nil && index < *input.Offset {
				continue
			}
			if x.id <= input.Cursor {
				continue
			}
			if len(items) == limit {
				next = last
				break
			}
			items = append(items, x.v)
			last = x.id
		}
		data := map[string]any{"version": cfg.Version, "items": items, "next_cursor": next}
		if c.CapabilityID == "models.list" {
			data["total"] = len(all)
		}
		return Success(data, time.Now()), nil
	}
	if resource == nil {
		return Result{}, routing.Fail("not_found", "resource not found")
	}
	return Success(map[string]any{"version": cfg.Version, "resource": resource}, time.Now()), nil
}

// Search the fields and labels shown in the model list before pagination.
func modelMatches(m routing.Model, query, language string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	status := "Disabled"
	if m.Enabled {
		status = "Enabled"
	}
	tools, images, structured, empty := "Tools", "Images", "Structured output", "No description yet"
	if language == "zh" {
		status = "已禁用"
		if m.Enabled {
			status = "已启用"
		}
		tools, images, structured, empty = "工具", "图片", "结构化输出", "暂无描述"
	}
	description := m.Description
	if description == "" {
		description = empty
	}
	fields := []string{m.ID, m.ProviderID + " / " + m.UpstreamName, description, m.Location, status}
	if m.Capabilities.Tools {
		fields = append(fields, tools)
	}
	if m.Capabilities.Images {
		fields = append(fields, images)
	}
	if m.Capabilities.StructuredOutput {
		fields = append(fields, structured)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}
