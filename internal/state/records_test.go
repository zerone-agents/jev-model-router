package state

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultRetentionSevenDays(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	s, e := Open(filepath.Join(t.TempDir(), "db"), func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i, age := range []time.Duration{8 * 24 * time.Hour, 6 * 24 * time.Hour} {
		if e = s.Append(context.Background(), routing.Record{RequestID: fmt.Sprint(i), CreatedAt: now.Add(-age)}); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Prune(context.Background(), now, 0); e != nil {
		t.Fatal(e)
	}
	p, e := s.ListRecords(context.Background(), "", 50)
	if e != nil || len(p.Records) != 1 || p.Records[0].RequestID != "1" {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestRecordPaginationBounded(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), time.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		if e = s.Append(context.Background(), routing.Record{RequestID: fmt.Sprint(i), CreatedAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	a, e := s.ListRecords(context.Background(), "", 2)
	if e != nil || len(a.Records) != 2 || a.NextCursor == "" {
		t.Fatalf("%+v %v", a, e)
	}
	b, e := s.ListRecords(context.Background(), a.NextCursor, 2)
	if e != nil || len(b.Records) != 1 || b.Records[0].RequestID == a.Records[0].RequestID {
		t.Fatalf("%+v %v", b, e)
	}
	if _, e = s.ListRecords(context.Background(), "", 201); e == nil {
		t.Fatal("unbounded limit")
	}
}

func TestRecordCountRetentionAndOffset(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureRecordLimit(ctx, 3); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		if e = s.Append(ctx, routing.Record{RequestID: fmt.Sprint(i), CreatedAt: now}); e != nil {
			t.Fatal(e)
		}
	}
	p, e := s.ListRecords(ctx, "", 2, 1)
	if e != nil || p.Total != 3 || len(p.Records) != 2 || p.Records[0].RequestID != "6" || p.Records[1].RequestID != "5" {
		t.Fatalf("page: %+v %v", p, e)
	}
	for _, v := range []struct {
		cursor string
		offset int
	}{{"", -1}, {"3", 1}} {
		if _, e = s.ListRecords(ctx, v.cursor, 2, v.offset); e == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
	p, e = s.ListRecords(ctx, "", 2, 99)
	if e != nil || p.Total != 3 || len(p.Records) != 0 {
		t.Fatalf("past end %+v %v", p, e)
	}
	s.Close()
	s, e = Open(path, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.ConfigureRecordLimit(ctx, 2); e != nil {
		t.Fatal(e)
	}
	p, e = s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 2 || p.Records[1].RequestID != "6" {
		t.Fatalf("restart %+v %v", p, e)
	}
	if e = s.Prune(ctx, now.Add(8*24*time.Hour), 7); e != nil {
		t.Fatal(e)
	}
	p, e = s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 0 || len(p.Records) != 0 {
		t.Fatalf("pruned count %+v %v", p, e)
	}
	if e = s.Append(ctx, routing.Record{RequestID: "new", CreatedAt: now}); e != nil {
		t.Fatal(e)
	}
	p, e = s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 1 {
		t.Fatalf("after prune %+v %v", p, e)
	}
}

func TestRecordRetentionBatchesAndLegacy(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	s, e := Open(filepath.Join(t.TempDir(), "db"), time.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// Seed an upgraded database with more rows than one cleanup batch.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < recordPruneBatch+5; i++ {
		if _, e = tx.Exec(`INSERT INTO routing_records(request_id,created_ns,body) VALUES(?,?,?)`, fmt.Sprint(i), now.Add(-8*24*time.Hour).UnixNano(), `{"request_id":"old","generation_ms":900,"decision_ms":3}`); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureRecordLimit(ctx, 2); e != nil {
		t.Fatal(e)
	}
	p, e := s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 2 || p.Records[0].RequestSummary != "" || p.Records[0].DecisionMillis != 3 {
		t.Fatalf("legacy %+v %v", p, e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "generation_ms") {
		t.Fatal("legacy generation timing leaked")
	}
	if e = s.Prune(ctx, now, 7); e != nil {
		t.Fatal(e)
	}
	p, e = s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 0 {
		t.Fatalf("expiry %+v %v", p, e)
	}
}

func TestRecordCountMigrationAndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Append(ctx, routing.Record{RequestID: "old", CreatedAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	// Recreate the pre-counter schema with an existing record, then reopen.
	for _, q := range []string{`DROP TRIGGER routing_record_added`, `DROP TRIGGER routing_record_removed`, `DROP TABLE routing_record_count`, `DELETE FROM schema_migrations WHERE name='004_record_count.sql'`} {
		if _, e = s.db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s, e = Open(path, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 1 {
		t.Fatalf("migration %+v %v", p, e)
	}
	if e = s.ConfigureRecordLimit(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`CREATE TRIGGER reject_record_removal BEFORE DELETE ON routing_records BEGIN SELECT RAISE(ABORT,'test'); END`); e != nil {
		t.Fatal(e)
	}
	if e = s.Append(ctx, routing.Record{RequestID: "new", CreatedAt: time.Now()}); e == nil {
		t.Fatal("expected rollback")
	}
	p, e = s.ListRecords(ctx, "", 20)
	if e != nil || p.Total != 1 || len(p.Records) != 1 || p.Records[0].RequestID != "old" {
		t.Fatalf("rollback %+v %v", p, e)
	}
}
