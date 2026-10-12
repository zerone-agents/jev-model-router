package state

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/health"
)

func (s *Store) LoadHealthKey(ctx context.Context) ([32]byte, error) {
	var key [32]byte
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return key, health.ErrUnavailable
	}
	defer tx.Rollback()
	var b []byte
	var version int
	e = tx.QueryRowContext(ctx, "SELECT fingerprint_key,version FROM health_meta WHERE id=1").Scan(&b, &version)
	if errors.Is(e, sql.ErrNoRows) {
		var n int
		if tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM health_records)+(SELECT count(*) FROM health_initialized)").Scan(&n) != nil || n != 0 {
			return key, health.ErrUnavailable
		}
		// A migration receipt alone does not mean initialization succeeded.
		if _, e = rand.Read(key[:]); e != nil {
			return key, health.ErrUnavailable
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO health_meta VALUES(1,?,1)", key[:]); e != nil {
			return [32]byte{}, health.ErrUnavailable
		}
		digest := sha256.Sum256(key[:])
		if _, e = tx.ExecContext(ctx, "INSERT INTO health_initialized VALUES(1,?)", digest[:]); e != nil {
			return [32]byte{}, health.ErrUnavailable
		}
	} else if e != nil || version != 1 || len(b) != 32 {
		return key, health.ErrUnavailable
	} else {
		copy(key[:], b)
		var checksum []byte
		digest := sha256.Sum256(key[:])
		if tx.QueryRowContext(ctx, "SELECT key_hash FROM health_initialized WHERE id=1").Scan(&checksum) != nil || !bytes.Equal(checksum, digest[:]) {
			return [32]byte{}, health.ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return [32]byte{}, health.ErrUnavailable
	}
	return key, nil
}
func (s *Store) LoadHealth(ctx context.Context) ([]health.Record, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT body FROM health_records")
	if e != nil {
		return nil, health.ErrUnavailable
	}
	defer rows.Close()
	records := []health.Record{}
	for rows.Next() {
		var b []byte
		var r health.Record
		if rows.Scan(&b) != nil || json.Unmarshal(b, &r) != nil {
			return nil, health.ErrUnavailable
		}
		records = append(records, r)
	}
	if rows.Err() != nil {
		return nil, health.ErrUnavailable
	}
	return records, nil
}
func (s *Store) SaveHealth(ctx context.Context, r health.Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return health.ErrUnavailable
	}
	_, e = s.db.ExecContext(ctx, "INSERT INTO health_records(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(r.ID), string(b))
	if e != nil {
		return health.ErrUnavailable
	}
	return nil
}
func (s *Store) DeleteHealth(ctx context.Context, id health.TargetID) error {
	_, e := s.db.ExecContext(ctx, "DELETE FROM health_records WHERE id=?", string(id))
	if e != nil {
		return health.ErrUnavailable
	}
	return nil
}
