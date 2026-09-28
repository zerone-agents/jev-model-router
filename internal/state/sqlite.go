package state

import (
	"context"
	"database/sql"
	"embed"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/templates"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db  *sql.DB
	mu  sync.Mutex
	now func() time.Time
}

func Open(path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(filepath.Dir(absolute), 0700); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: absolute, RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)"}
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, storageError()
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: now}
	if e = s.migrate(); e != nil {
		db.Close()
		return nil, e
	}
	if e = os.Chmod(absolute, 0600); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) migrate() error {
	ctx := context.Background()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return storageError()
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); e != nil {
		return storageError()
	}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		var n int
		if e = tx.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name=?`, entry.Name()).Scan(&n); e != nil {
			return storageError()
		}
		if n > 0 {
			continue
		}
		b, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			return e
		}
		if _, e = tx.Exec(string(b)); e != nil {
			return storageError()
		}
		if _, e = tx.Exec(`INSERT INTO schema_migrations(name) VALUES(?)`, entry.Name()); e != nil {
			return storageError()
		}
	}
	if _, e = tx.Exec(`INSERT OR IGNORE INTO config_meta(id,version,prompt,decision) VALUES(1,1,?,'{}')`, templates.Balanced()); e != nil {
		return storageError()
	}
	if e = tx.Commit(); e != nil {
		return storageError()
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }
func storageError() error     { return routing.Fail("storage_error", "configuration storage unavailable") }
