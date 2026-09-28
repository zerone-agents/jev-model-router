package state

import (
	"context"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"path/filepath"
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
