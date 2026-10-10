package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) Snapshot(ctx context.Context) (routing.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshot(ctx, s.db)
}
func snapshot(ctx context.Context, q queryer) (routing.Snapshot, error) {
	s := routing.Snapshot{Providers: []routing.Provider{}, Models: []routing.Model{}}
	var d string
	if e := q.QueryRowContext(ctx, `SELECT version,prompt,decision FROM config_meta WHERE id=1`).Scan(&s.Version, &s.Prompt, &d); e != nil {
		return s, storageError()
	}
	if json.Unmarshal([]byte(d), &s.Decision) != nil {
		return s, storageError()
	}
	for _, table := range []string{"providers", "models"} {
		rows, e := q.QueryContext(ctx, `SELECT body FROM `+table+` ORDER BY id`)
		if e != nil {
			return s, storageError()
		}
		for rows.Next() {
			var b []byte
			if rows.Scan(&b) != nil {
				rows.Close()
				return s, storageError()
			}
			if table == "providers" {
				var p routing.Provider
				if json.Unmarshal(b, &p) != nil {
					rows.Close()
					return s, storageError()
				}
				p.Protocol = p.EffectiveProtocol()
				s.Providers = append(s.Providers, p)
			} else {
				var m routing.Model
				if json.Unmarshal(b, &m) != nil {
					rows.Close()
					return s, storageError()
				}
				s.Models = append(s.Models, m)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return s, storageError()
		}
	}
	return s, nil
}
func (s *Store) Apply(ctx context.Context, principal string, c management.Call, validate func(routing.Snapshot) error) (management.Result, error) {
	envelope, _ := json.Marshal(c)
	if contracts.Validate("write_envelope", envelope) != nil {
		return management.Result{}, routing.Fail("invalid_request", "expected_version and idempotency_key required")
	}
	v, e := contracts.Decode(c.Input)
	if e != nil {
		return management.Result{}, routing.Fail("invalid_request", "invalid JSON input")
	}
	canonical, _ := json.Marshal([]any{c.CapabilityID, *c.ExpectedVersion, v})
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	if c.CapabilityID == "providers.put" {
		if obj, ok := v.(map[string]any); ok {
			if _, hasKey := obj["api_key"]; hasKey {
				if s.cipher == nil {
					return management.Result{}, credentialUnavailable()
				}
				digest = s.cipher.Digest(canonical)
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return management.Result{}, storageError()
	}
	defer tx.Rollback()
	var oldHash, oldResult string
	var expires int64
	e = tx.QueryRowContext(ctx, `SELECT digest,result,expires_ns FROM idempotency WHERE principal=? AND key=?`, principal, c.IdempotencyKey).Scan(&oldHash, &oldResult, &expires)
	if e == nil && expires > s.now().UnixNano() {
		if oldHash != digest {
			return management.Result{}, routing.Fail("idempotency_conflict", "key belongs to another operation")
		}
		var r management.Result
		if json.Unmarshal([]byte(oldResult), &r) != nil {
			return r, storageError()
		}
		return r, nil
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return management.Result{}, storageError()
	}
	current, e := snapshot(ctx, tx)
	if e != nil {
		return management.Result{}, e
	}
	if current.Version != *c.ExpectedVersion {
		return management.Result{}, routing.Fail("config_conflict", "configuration version changed")
	}
	if e = contracts.Validate(c.CapabilityID, c.Input); e != nil {
		return management.Result{}, routing.Fail("invalid_request", "input does not match capability schema")
	}
	prepared, e := s.prepareProvider(ctx, tx, c)
	if e != nil {
		return management.Result{}, e
	}
	resource, e := mutate(&current, prepared)
	if e != nil {
		return management.Result{}, e
	}
	if e = routing.ValidateSnapshot(current); e != nil {
		return management.Result{}, e
	}
	if e = s.validateManaged(ctx, tx, current); e != nil {
		return management.Result{}, e
	}
	if validate != nil {
		if e = validate(current); e != nil {
			return management.Result{}, e
		}
	}
	current.Version++
	if e = save(ctx, tx, current); e != nil {
		return management.Result{}, e
	}
	r := management.Success(map[string]any{"version": current.Version, "resource": resource}, s.now())
	encoded, _ := json.Marshal(r)
	if _, e = tx.ExecContext(ctx, `DELETE FROM idempotency WHERE expires_ns<=?`, s.now().UnixNano()); e != nil {
		return r, storageError()
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotency(principal,key,digest,result,expires_ns) VALUES(?,?,?,?,?)`, principal, c.IdempotencyKey, digest, string(encoded), s.now().Add(contracts.IdempotencyTTL()).UnixNano()); e != nil {
		return r, storageError()
	}
	if e = tx.Commit(); e != nil {
		return management.Result{}, storageError()
	}
	return r, nil
}
func mutate(s *routing.Snapshot, c management.Call) (any, error) {
	var id struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.Input, &id)
	switch c.CapabilityID {
	case "providers.put":
		var p routing.Provider
		json.Unmarshal(c.Input, &p)
		p.Protocol = p.EffectiveProtocol()
		found := false
		for i, x := range s.Providers {
			if x.ID == p.ID {
				s.Providers[i] = p
				found = true
			}
		}
		if !found {
			s.Providers = append(s.Providers, p)
		}
		return p, nil
	case "models.put":
		var m routing.Model
		json.Unmarshal(c.Input, &m)
		found := false
		for i, x := range s.Models {
			if x.ID == m.ID {
				s.Models[i] = m
				found = true
			}
		}
		if !found {
			s.Models = append(s.Models, m)
		}
		return m, nil
	case "providers.delete":
		found := false
		for i, p := range s.Providers {
			if p.ID == id.ID {
				s.Providers = append(s.Providers[:i], s.Providers[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil, routing.Fail("not_found", "provider not found")
		}
	case "models.delete":
		found := false
		for i, m := range s.Models {
			if m.ID == id.ID {
				s.Models = append(s.Models[:i], s.Models[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil, routing.Fail("not_found", "model not found")
		}
	case "decision.put":
		s.Decision = routing.DecisionConfig{}
		json.Unmarshal(c.Input, &s.Decision)
		return s.Decision, nil
	case "prompt.put":
		var p struct {
			Text string `json:"text"`
		}
		json.Unmarshal(c.Input, &p)
		s.Prompt = p.Text
		return p, nil
	default:
		return nil, routing.Fail("invalid_request", "not a configuration write")
	}
	return map[string]string{"id": id.ID}, nil
}
func save(ctx context.Context, tx *sql.Tx, s routing.Snapshot) error {
	for _, table := range []string{"models", "providers"} {
		if _, e := tx.ExecContext(ctx, `DELETE FROM `+table); e != nil {
			return storageError()
		}
	}
	for _, p := range s.Providers {
		b, _ := json.Marshal(p)
		if _, e := tx.ExecContext(ctx, `INSERT INTO providers(id,body) VALUES(?,?)`, p.ID, string(b)); e != nil {
			return storageError()
		}
	}
	for _, m := range s.Models {
		b, _ := json.Marshal(m)
		if _, e := tx.ExecContext(ctx, `INSERT INTO models(id,provider_id,body) VALUES(?,?,?)`, m.ID, m.ProviderID, string(b)); e != nil {
			return storageError()
		}
	}
	d, _ := json.Marshal(s.Decision)
	if _, e := tx.ExecContext(ctx, `UPDATE config_meta SET version=?,prompt=?,decision=? WHERE id=1`, s.Version, s.Prompt, string(d)); e != nil {
		return storageError()
	}
	return nil
}

var _ management.ConfigStore = (*Store)(nil)
