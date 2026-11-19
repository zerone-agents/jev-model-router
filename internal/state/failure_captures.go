package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

const captureRetention = 24 * time.Hour
const captureCapacity = 64 << 20

type FailureCapture struct {
	RequestID string          `json:"request_id"`
	CreatedAt time.Time       `json:"created_at"`
	Evidence  json.RawMessage `json:"evidence"`
}

func (s *Store) SaveFailureCapture(ctx context.Context, capture FailureCapture) error {
	err := s.saveFailureCapture(ctx, capture, captureCapacity)
	s.failureCaptureDegraded.Store(err != nil)
	return err
}

func (s *Store) saveFailureCapture(ctx context.Context, capture FailureCapture, capacity int) error {
	body, err := json.Marshal(capture)
	if err != nil {
		return storageError()
	}
	charge := max(len(body), 1024)
	if charge > capacity {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError()
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM failure_captures WHERE created_ns <= ?`, s.now().Add(-captureRetention).UnixNano()); err != nil {
		return storageError()
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO failure_captures(request_id,created_ns,body,charge)
VALUES (?,?,?,?) ON CONFLICT(request_id) DO NOTHING`, capture.RequestID, capture.CreatedAt.UnixNano(), string(body), charge)
	if err != nil {
		return storageError()
	}
	if inserted, err := result.RowsAffected(); err != nil {
		return storageError()
	} else if inserted != 0 {
		// Keep the newest suffix that fits, counting the full row charge.
		if _, err := tx.ExecContext(ctx, `DELETE FROM failure_captures WHERE sequence IN (
SELECT sequence FROM (
  SELECT sequence, SUM(charge) OVER (ORDER BY sequence DESC ROWS UNBOUNDED PRECEDING) AS retained_bytes
  FROM failure_captures
) WHERE retained_bytes > ?)`, capacity); err != nil {
			return storageError()
		}
	}
	if tx.Commit() != nil {
		return storageError()
	}
	return nil
}

func (s *Store) DeleteFailureCapture(ctx context.Context, requestID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM failure_captures WHERE request_id=?`, requestID); err != nil {
		return storageError()
	}
	return nil
}

type FailureCaptureSummary struct {
	Sequence  int64     `json:"sequence"`
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) GetFailureCapture(ctx context.Context, requestID string) (FailureCapture, error) {
	var body string
	var capture FailureCapture
	err := s.db.QueryRowContext(ctx, `SELECT body FROM failure_captures
WHERE request_id=? AND created_ns>?`, requestID, s.now().Add(-captureRetention).UnixNano()).Scan(&body)
	if err == sql.ErrNoRows {
		return capture, routing.Fail("not_found", "failure capture not found")
	}
	if err != nil || json.Unmarshal([]byte(body), &capture) != nil {
		return capture, storageError()
	}
	return capture, nil
}

func (s *Store) ListFailureCaptures(ctx context.Context, before int64, limit int) ([]FailureCaptureSummary, error) {
	if limit < 1 || limit > 200 || before < 0 {
		return nil, routing.Fail("invalid_request", "invalid failure capture pagination")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,request_id,created_ns FROM failure_captures
WHERE created_ns>? AND (?=0 OR sequence<?) ORDER BY sequence DESC LIMIT ?`,
		s.now().Add(-captureRetention).UnixNano(), before, before, limit)
	if err != nil {
		return nil, storageError()
	}
	defer rows.Close()
	items := make([]FailureCaptureSummary, 0, limit)
	for rows.Next() {
		var item FailureCaptureSummary
		var created int64
		if rows.Scan(&item.Sequence, &item.RequestID, &created) != nil {
			return nil, storageError()
		}
		item.CreatedAt = time.Unix(0, created).UTC()
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, storageError()
	}
	return items, nil
}

func (s *Store) pruneFailureCaptures(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM failure_captures WHERE created_ns<=?`, now.Add(-captureRetention).UnixNano()); err != nil {
		return storageError()
	}
	return nil
}

func (s *Store) FailureCaptureDegraded() bool { return s.failureCaptureDegraded.Load() }
