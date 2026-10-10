package state

import (
	"context"
	"database/sql"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"time"
)

func (s *Store) PlaygroundQuota(ctx context.Context, id string, now time.Time, l playground.Limits) (playground.Quota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return playground.Quota{}, storageError()
	}
	defer tx.Rollback()
	q, _, e := playgroundQuota(ctx, tx, id, now, l)
	return q, e
}
func (s *Store) AdmitPlayground(ctx context.Context, id string, now time.Time, l playground.Limits) (playground.Quota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return playground.Quota{}, storageError()
	}
	defer tx.Rollback()
	q, denied, e := playgroundQuota(ctx, tx, id, now, l)
	if e != nil {
		return q, e
	}
	if denied != nil {
		return q, denied
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM playground_admissions WHERE accepted_at<=?`, now.Add(-time.Minute).UnixNano()); e != nil {
		return q, storageError()
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM playground_days WHERE day<?`, now.UTC().Format("2006-01-02")); e != nil {
		return q, storageError()
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO playground_admissions(session_id,accepted_at) VALUES(?,?)`, id, now.UnixNano()); e != nil {
		return q, storageError()
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO playground_days(day,count) VALUES(?,1) ON CONFLICT(day) DO UPDATE SET count=count+1`, now.UTC().Format("2006-01-02")); e != nil {
		return q, storageError()
	}
	if e = tx.Commit(); e != nil {
		return q, storageError()
	}
	q.SessionRemaining--
	q.InstanceRemaining--
	q.DayRemaining--
	return q, nil
}
func playgroundQuota(ctx context.Context, tx *sql.Tx, id string, now time.Time, l playground.Limits) (playground.Quota, *playground.LimitError, error) {
	now = now.UTC()
	day := now.Format("2006-01-02")
	reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	q := playground.Quota{ResetAt: reset}
	var total, session, daily int
	if e := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN session_id=? THEN 1 ELSE 0 END),0) FROM playground_admissions WHERE accepted_at>?`, id, now.Add(-time.Minute).UnixNano()).Scan(&total, &session); e != nil {
		return q, nil, storageError()
	}
	if e := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT count FROM playground_days WHERE day=?),0)`, day).Scan(&daily); e != nil {
		return q, nil, storageError()
	}
	q.SessionRemaining = max(0, l.SessionRPM-session)
	q.InstanceRemaining = max(0, l.InstanceRPM-total)
	q.DayRemaining = max(0, l.DailyRequests-daily)
	var denied *playground.LimitError
	consider := func(scope string, end time.Time) {
		wait := end.Sub(now)
		if denied == nil || wait > denied.RetryAfter {
			denied = &playground.LimitError{Scope: scope, RetryAfter: wait, ResetAt: &end}
		}
	}
	if daily >= l.DailyRequests {
		consider("day", reset)
	}
	// The offset also handles a limit lowered while older admissions remain.
	for _, w := range []struct {
		scope        string
		count, limit int
		session      bool
	}{{"session_minute", session, l.SessionRPM, true}, {"instance_minute", total, l.InstanceRPM, false}} {
		if w.count < w.limit {
			continue
		}
		query := `SELECT accepted_at FROM playground_admissions WHERE accepted_at>?`
		args := []any{now.Add(-time.Minute).UnixNano()}
		if w.session {
			query += ` AND session_id=?`
			args = append(args, id)
		}
		query += ` ORDER BY accepted_at LIMIT 1 OFFSET ?`
		args = append(args, w.count-w.limit)
		var oldest int64
		if e := tx.QueryRowContext(ctx, query, args...).Scan(&oldest); e != nil {
			return q, nil, storageError()
		}
		consider(w.scope, time.Unix(0, oldest).Add(time.Minute))
	}
	return q, denied, nil
}
