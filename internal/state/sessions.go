package state

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/session"
	"time"
)

func sessionVerifier(key [32]byte, salt []byte) []byte {
	h := hmac.New(sha256.New, key[:])
	h.Write([]byte("jev-router/dashboard/authority/v1\x00"))
	h.Write(salt)
	return h.Sum(nil)
}
func sessionUnauthorized() error { return routing.Fail("unauthorized", "session unavailable") }
func (s *Store) PrepareSessions(ctx context.Context, key [32]byte, policy string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", storageError()
	}
	defer tx.Rollback()
	var salt, verifier []byte
	var generation, oldPolicy string
	err = tx.QueryRowContext(ctx, "SELECT salt,verifier,generation,origin_policy FROM session_auth WHERE id=1").Scan(&salt, &verifier, &generation, &oldPolicy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", storageError()
	}
	if err != nil || len(salt) != 32 || generation == "" || oldPolicy != policy || !hmac.Equal(verifier, sessionVerifier(key, salt)) {
		salt = make([]byte, 32)
		g := make([]byte, 32)
		if _, err = rand.Read(salt); err != nil {
			return "", storageError()
		}
		if _, err = rand.Read(g); err != nil {
			return "", storageError()
		}
		generation = hex.EncodeToString(g)
		if _, err = tx.ExecContext(ctx, "DELETE FROM dashboard_sessions"); err != nil {
			return "", storageError()
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO session_auth(id,salt,verifier,generation,origin_policy) VALUES(1,?,?,?,?)", salt, sessionVerifier(key, salt), generation, policy); err != nil {
			return "", storageError()
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM dashboard_sessions WHERE expires_ns<=?", s.now().UnixNano()); err != nil {
		return "", storageError()
	}
	if err = tx.Commit(); err != nil {
		return "", storageError()
	}
	return generation, nil
}
func (s *Store) CreateSession(ctx context.Context, r session.Record, oldHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError()
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM session_auth WHERE id=1").Scan(&generation); err != nil {
		return storageError()
	}
	if generation != r.Generation {
		return sessionUnauthorized()
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM dashboard_sessions WHERE expires_ns<=?", s.now().UnixNano()); err != nil {
		return storageError()
	}
	if oldHash != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM dashboard_sessions WHERE hash=? AND generation=? AND origin=?", oldHash, r.Generation, r.Origin); err != nil {
			return storageError()
		}
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM dashboard_sessions").Scan(&n); err != nil {
		return storageError()
	}
	if n >= contracts.SessionLimits().MaxSessions {
		return routing.Fail("session_limit", "session capacity reached; log out an existing session or wait for expiry")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO dashboard_sessions(hash,generation,origin,created_ns,expires_ns) VALUES(?,?,?,?,?)", r.Hash, r.Generation, r.Origin, r.CreatedAt.UnixNano(), r.ExpiresAt.UnixNano()); err != nil {
		return storageError()
	}
	if tx.Commit() != nil {
		return storageError()
	}
	return nil
}
func (s *Store) GetSession(ctx context.Context, hash string) (session.Record, error) {
	var r session.Record
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT s.hash,s.generation,s.origin,s.created_ns,s.expires_ns FROM dashboard_sessions s JOIN session_auth a ON a.id=1 AND a.generation=s.generation WHERE s.hash=?`, hash).Scan(&r.Hash, &r.Generation, &r.Origin, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return r, sessionUnauthorized()
	}
	if err != nil {
		return r, storageError()
	}
	r.CreatedAt = time.Unix(0, created)
	r.ExpiresAt = time.Unix(0, expires)
	return r, nil
}
func (s *Store) DeleteSession(ctx context.Context, hash, generation string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `DELETE FROM dashboard_sessions WHERE hash=? AND generation=? AND generation=(SELECT generation FROM session_auth WHERE id=1)`, hash, generation)
	if err != nil {
		return storageError()
	}
	n, err := res.RowsAffected()
	if err != nil {
		return storageError()
	}
	if n == 0 {
		return sessionUnauthorized()
	}
	return nil
}
