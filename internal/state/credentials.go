package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func credentialUnavailable() error {
	return routing.Fail("config_missing", "managed credential unavailable")
}

// ResolveManaged resolves an immutable revision, never the provider's latest key.
func (s *Store) ResolveManaged(ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveManaged(context.Background(), s.db, ref, "")
}
func (s *Store) resolveManaged(ctx context.Context, q queryer, ref, owner string) ([]byte, error) {
	if s.cipher == nil || !strings.HasPrefix(ref, "managed:") {
		return nil, credentialUnavailable()
	}
	revision := strings.TrimPrefix(ref, "managed:")
	var providerID, envelope string
	if err := q.QueryRowContext(ctx, "SELECT provider_id,envelope FROM managed_credentials WHERE revision_id=?", revision).Scan(&providerID, &envelope); err != nil {
		return nil, credentialUnavailable()
	}
	if owner != "" && owner != providerID {
		return nil, routing.Fail("invalid_request", "managed credential does not belong to resource")
	}
	key, err := s.cipher.Open(providerID, revision, envelope)
	if err != nil {
		return nil, credentialUnavailable()
	}
	return key, nil
}
func (s *Store) verifyCredentials(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT revision_id,provider_id,envelope FROM managed_credentials")
	if err != nil {
		return storageError()
	}
	defer rows.Close()
	for rows.Next() {
		var revision, owner, envelope string
		if rows.Scan(&revision, &owner, &envelope) != nil {
			return storageError()
		}
		if s.cipher == nil {
			return credentialUnavailable()
		}
		if _, err = s.cipher.Open(owner, revision, envelope); err != nil {
			return credentialUnavailable()
		}
	}
	if rows.Err() != nil {
		return storageError()
	}
	return nil
}

// prepareCredential stores only authenticated ciphertext and replaces the sensitive
// write DTO with a public resource before mutation or receipt serialization.
func (s *Store) prepareCredential(ctx context.Context, tx *sql.Tx, c management.Call) (management.Call, error) {
	if c.CapabilityID != "providers.put" && c.CapabilityID != "decision.put" {
		return c, nil
	}
	var input struct {
		Model     string                   `json:"model"`
		Path      string                   `json:"path"`
		Protocol  routing.ProviderProtocol `json:"protocol"`
		ID        string                   `json:"id"`
		BaseURL   string                   `json:"base_url"`
		SecretRef string                   `json:"secret_ref"`
		APIKey    *string                  `json:"api_key"`
	}
	if json.Unmarshal(c.Input, &input) != nil {
		return c, routing.Fail("invalid_request", "invalid credential input")
	}
	owner := input.ID
	if c.CapabilityID == "decision.put" {
		owner = decisionCredentialOwner
	}
	if input.APIKey != nil {
		if s.cipher == nil {
			return c, credentialUnavailable()
		}
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return c, routing.Fail("internal_error", "credential encryption failed")
		}
		revision := hex.EncodeToString(random)
		envelope, err := s.cipher.Seal(owner, revision, []byte(*input.APIKey))
		if err != nil {
			return c, routing.Fail("internal_error", "credential encryption failed")
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO managed_credentials(revision_id,provider_id,envelope) VALUES(?,?,?)", revision, owner, envelope); err != nil {
			return c, storageError()
		}
		input.SecretRef = "managed:" + revision
	}
	if c.CapabilityID == "decision.put" {
		c.Input, _ = json.Marshal(routing.DecisionConfig{BaseURL: input.BaseURL, Path: input.Path, Model: input.Model, SecretRef: input.SecretRef})
		return c, nil
	}
	c.Input, _ = json.Marshal(routing.Provider{Protocol: input.Protocol, ID: input.ID, BaseURL: input.BaseURL, SecretRef: input.SecretRef})
	return c, nil
}

// The leading colon cannot occur in a provider ID.
const decisionCredentialOwner = ":decision"

func (s *Store) validateManaged(ctx context.Context, q queryer, cfg routing.Snapshot) error {
	if strings.HasPrefix(cfg.Decision.SecretRef, "managed:") {
		if _, err := s.resolveManaged(ctx, q, cfg.Decision.SecretRef, decisionCredentialOwner); err != nil {
			return err
		}
	}
	for _, p := range cfg.Providers {
		if strings.HasPrefix(p.SecretRef, "managed:") {
			if _, err := s.resolveManaged(ctx, q, p.SecretRef, p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
