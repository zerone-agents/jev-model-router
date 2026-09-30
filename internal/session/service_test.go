package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"testing"
	"time"
)

type memoryStore struct{ records map[string]Record }

func (m *memoryStore) PrepareSessions(context.Context, [32]byte, string) (string, error) {
	return "generation", nil
}
func (m *memoryStore) CreateSession(_ context.Context, r Record, old string) error {
	delete(m.records, old)
	m.records[r.Hash] = r
	return nil
}
func (m *memoryStore) GetSession(_ context.Context, h string) (Record, error) {
	r, ok := m.records[h]
	if !ok {
		return r, routing.Fail("unauthorized", "invalid session")
	}
	return r, nil
}
func (m *memoryStore) DeleteSession(_ context.Context, h, g string) error {
	delete(m.records, h)
	return nil
}
func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	start := now
	store := &memoryStore{records: map[string]Record{}}
	s, err := New(ctx, store, sha256.Sum256([]byte("settings")), "", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Login(ctx, "http://localhost", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Login(ctx, "http://localhost", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(a.Token)
	if err != nil || len(raw) != 32 || a.Token == b.Token {
		t.Fatal("invalid token")
	}
	encoded, _ := json.Marshal(a)
	if strings.Contains(string(encoded), a.Token) {
		t.Fatal("token serialized")
	}
	if len(store.records) != 2 {
		t.Fatal("missing sessions")
	}
	for _, r := range store.records {
		if r.Hash == a.Token || !r.ExpiresAt.Equal(start.Add(24*time.Hour)) {
			t.Fatal("invalid record")
		}
	}
	info, err := s.Status(ctx, "http://localhost", a.Token)
	if err != nil || !s.CheckCSRF(a.Token, info.CSRFToken) || s.CheckCSRF(b.Token, info.CSRFToken) {
		t.Fatal("csrf")
	}
	for _, token := range []string{"", a.Token + "=", a.Token + "\n", a.Token[:42]} {
		if _, err := s.Status(ctx, "http://localhost", token); err == nil {
			t.Fatal("accepted malformed token")
		}
	}
	if _, err := s.Status(ctx, "https://localhost", a.Token); err == nil {
		t.Fatal("origin")
	}
	if err := s.Logout(ctx, "http://localhost", b.Token, info.CSRFToken); err == nil {
		t.Fatal("stale csrf")
	}
	if _, err := s.Status(ctx, "http://localhost", b.Token); err != nil {
		t.Fatal("revoked other session")
	}
	now = start.Add(23 * time.Hour)
	if _, err := s.Status(ctx, "http://localhost", a.Token); err != nil {
		t.Fatal(err)
	}
	now = start.Add(24 * time.Hour)
	if _, err := s.Status(ctx, "http://localhost", a.Token); err == nil {
		t.Fatal("sliding expiry")
	}
	now = start
	if err := s.Logout(ctx, "http://localhost", a.Token, info.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(ctx, "http://localhost", a.Token); err == nil {
		t.Fatal("logout ineffective")
	}
	for h, r := range store.records {
		r.Generation = "old"
		store.records[h] = r
	}
	if _, err := s.Status(ctx, "http://localhost", b.Token); err == nil {
		t.Fatal("old generation")
	}
}
