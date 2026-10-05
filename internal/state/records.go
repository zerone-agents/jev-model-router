package state

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strconv"
	"time"
)

const DefaultRecordLimit = 100000
const recordPruneBatch = 1000

type RecordPage struct {
	Total      int              `json:"total"`
	Records    []routing.Record `json:"records"`
	NextCursor string           `json:"next_cursor"`
}

func (s *Store) Append(ctx context.Context, r routing.Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return storageError()
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return storageError()
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO routing_records(request_id,created_ns,body) VALUES(?,?,?)`, r.RequestID, r.CreatedAt.UnixNano(), string(b))
	if e != nil {
		return storageError()
	}

	// The counter and insert/delete triggers run inside this transaction. Usually
	// this removes zero or one row, with no full-table count or deep offset scan.
	_, e = tx.ExecContext(ctx, `DELETE FROM routing_records WHERE sequence IN
 (SELECT sequence FROM routing_records ORDER BY sequence LIMIT
 MAX(0,(SELECT total FROM routing_record_count WHERE id=1)-?))`, s.recordLimit)
	if e != nil {
		return storageError()
	}
	if tx.Commit() != nil {
		return storageError()
	}
	return nil
}

// ConfigureRecordLimit is called once before serving requests. Existing excess
// records are removed in short transactions, so startup never needs a giant DELETE.
func (s *Store) ConfigureRecordLimit(ctx context.Context, max int) error {
	if max == 0 {
		max = DefaultRecordLimit
	}
	if max < 1 || max > 10000000 {
		return routing.Fail("invalid_request", "invalid record count limit")
	}
	for {
		result, err := s.db.ExecContext(ctx, `DELETE FROM routing_records WHERE sequence IN
   (SELECT sequence FROM routing_records ORDER BY sequence LIMIT
    MIN(?,MAX(0,(SELECT total FROM routing_record_count WHERE id=1)-?)))`, recordPruneBatch, max)
		if err != nil {
			return storageError()
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storageError()
		}
		if n == 0 {
			break
		}
	}
	s.recordLimit = max
	return nil
}
func (s *Store) ListRecords(ctx context.Context, cursor string, limit int, offsets ...int) (RecordPage, error) {
	out := RecordPage{Records: []routing.Record{}}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return out, routing.Fail("invalid_request", "record limit out of range")
	}
	offset := 0
	if len(offsets) > 1 {
		return out, routing.Fail("invalid_request", "invalid record offset")
	}
	if len(offsets) == 1 {
		offset = offsets[0]
	}
	if offset < 0 || (offset > 0 && cursor != "") {
		return out, routing.Fail("invalid_request", "invalid record pagination")
	}
	before := int64(1<<63 - 1)
	if cursor != "" {
		v, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || v < 1 {
			return out, routing.Fail("invalid_request", "invalid record cursor")
		}
		before = v
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, storageError()
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, `SELECT total FROM routing_record_count WHERE id=1`).Scan(&out.Total); e != nil {
		return out, storageError()
	}
	rows, e := tx.QueryContext(ctx, `SELECT sequence,body FROM routing_records WHERE sequence<? ORDER BY sequence DESC LIMIT ? OFFSET ?`, before, limit+1, offset)
	if e != nil {
		return out, storageError()
	}
	defer rows.Close()
	last := int64(0)
	for rows.Next() {
		var seq int64
		var b []byte
		if rows.Scan(&seq, &b) != nil {
			return out, storageError()
		}
		if len(out.Records) == limit {
			out.NextCursor = strconv.FormatInt(last, 10)
			break
		}
		var r routing.Record
		if json.Unmarshal(b, &r) != nil {
			return out, storageError()
		}
		out.Records = append(out.Records, r)
		last = seq
	}
	if rows.Err() != nil {
		return out, storageError()
	}
	return out, nil
}
func (s *Store) Prune(ctx context.Context, now time.Time, days int) error {
	if days == 0 {
		days = 7
	}
	if days < 1 || days > 3650 {
		return routing.Fail("invalid_request", "invalid retention")
	}
	for {
		result, e := s.db.ExecContext(ctx, `DELETE FROM routing_records WHERE sequence IN
   (SELECT sequence FROM routing_records WHERE created_ns<? ORDER BY created_ns LIMIT ?)`, now.Add(-time.Duration(days)*24*time.Hour).UnixNano(), recordPruneBatch)
		if e != nil {
			return storageError()
		}
		n, e := result.RowsAffected()
		if e != nil {
			return storageError()
		}
		if n < recordPruneBatch {
			return nil
		}
	}
}
