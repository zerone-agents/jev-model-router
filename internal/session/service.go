// Package session owns opaque dashboard authentication, independently of routing sessions.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"time"
)

type Record struct {
	Hash, Generation, Origin string
	CreatedAt, ExpiresAt     time.Time
}
type Store interface {
	PrepareSessions(context.Context, [32]byte, string) (string, error)
	CreateSession(context.Context, Record, string) error
	GetSession(context.Context, string) (Record, error)
	DeleteSession(context.Context, string, string) error
}
type Info struct {
	ExpiresAt time.Time `json:"expires_at"`
	CSRFToken string    `json:"csrf_token"`
}
type Issued struct {
	Info
	Token string `json:"-"`
}
type Service struct {
	store      Store
	generation string
	now        func() time.Time
}

func New(ctx context.Context, store Store, key [32]byte, policy string, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	g, err := store.PrepareSessions(ctx, key, policy)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, generation: g, now: now}, nil
}
func invalid() error { return routing.Fail("unauthorized", "session unavailable") }
func tokenBytes(token string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) != contracts.SessionLimits().TokenBytes || base64.RawURLEncoding.EncodeToString(b) != token {
		return nil, invalid()
	}
	return b, nil
}
func tokenHash(token string) (string, error) {
	b, err := tokenBytes(token)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func csrfToken(token string) string {
	b, err := tokenBytes(token)
	if err != nil {
		return ""
	}
	m := hmac.New(sha256.New, b)
	m.Write([]byte("jev-router/dashboard/csrf/v1"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *Service) CheckCSRF(token, csrf string) bool {
	expected := csrfToken(token)
	return expected != "" && hmac.Equal([]byte(expected), []byte(csrf))
}
func (s *Service) Login(ctx context.Context, origin, oldToken string) (Issued, error) {
	var oldHash string
	if oldToken != "" {
		if _, err := s.Status(ctx, origin, oldToken); err == nil {
			oldHash, _ = tokenHash(oldToken)
		} else if e, ok := err.(*routing.Error); !ok || e.Code != "unauthorized" {
			return Issued{}, err
		}
	}
	b := make([]byte, contracts.SessionLimits().TokenBytes)
	if _, err := rand.Read(b); err != nil {
		return Issued{}, routing.Fail("internal_error", "session unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	hash, _ := tokenHash(token)
	now := s.now().UTC()
	r := Record{Hash: hash, Generation: s.generation, Origin: origin, CreatedAt: now, ExpiresAt: now.Add(time.Duration(contracts.SessionLimits().TTLSeconds) * time.Second)}
	if err := s.store.CreateSession(ctx, r, oldHash); err != nil {
		return Issued{}, err
	}
	return Issued{Token: token, Info: Info{ExpiresAt: r.ExpiresAt, CSRFToken: csrfToken(token)}}, nil
}
func (s *Service) Status(ctx context.Context, origin, token string) (Info, error) {
	h, err := tokenHash(token)
	if err != nil {
		return Info{}, err
	}
	r, err := s.store.GetSession(ctx, h)
	if err != nil {
		return Info{}, err
	}
	if r.Hash != h || r.Generation != s.generation || r.Origin != origin || !s.now().Before(r.ExpiresAt) || r.CreatedAt.After(s.now()) {
		return Info{}, invalid()
	}
	return Info{ExpiresAt: r.ExpiresAt, CSRFToken: csrfToken(token)}, nil
}
func (s *Service) Logout(ctx context.Context, origin, token, csrf string) error {
	if _, err := s.Status(ctx, origin, token); err != nil {
		return err
	}
	if !s.CheckCSRF(token, csrf) {
		return routing.Fail("forbidden", "session changed; reconnect before retrying")
	}
	h, _ := tokenHash(token)
	return s.store.DeleteSession(ctx, h, s.generation)
}

// Identity validates a session before exposing its irreversible internal ID.
func (s *Service) Identity(ctx context.Context, origin, token string) (string, error) {
	if _, err := s.Status(ctx, origin, token); err != nil {
		return "", err
	}
	return tokenHash(token)
}
