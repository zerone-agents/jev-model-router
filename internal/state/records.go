package state

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strconv"
	"time"
)

type RecordPage struct {
	Records    []routing.Record `json:"records"`
	NextCursor string           `json:"next_cursor"`
}

func (s *Store) Append(ctx context.Context, r routing.Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return storageError()
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO routing_records(request_id,created_ns,body) VALUES(?,?,?)`, r.RequestID, r.CreatedAt.UnixNano(), string(b))
	if e != nil {
		return storageError()
	}
	return nil
}
func (s *Store) ListRecords(ctx context.Context, cursor string, limit int) (RecordPage, error) {
	out := RecordPage{Records: []routing.Record{}}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return out, routing.Fail("invalid_request", "record limit out of range")
	}
	before := int64(1<<63 - 1)
	if cursor != "" {
		v, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || v < 1 {
			return out, routing.Fail("invalid_request", "invalid record cursor")
		}
		before = v
	}
	rows, e := s.db.QueryContext(ctx, `SELECT sequence,body FROM routing_records WHERE sequence<? ORDER BY sequence DESC LIMIT ?`, before, limit+1)
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
	_, e := s.db.ExecContext(ctx, `DELETE FROM routing_records WHERE created_ns<?`, now.Add(-time.Duration(days)*24*time.Hour).UnixNano())
	if e != nil {
		return storageError()
	}
	return nil
}
