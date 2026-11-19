package state

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFailureCaptureMetadataCapacityAndExpiry(t *testing.T) {
	now := time.Now()
	db := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	ctx := context.Background()
	var writes sync.WaitGroup
	for index := range 16 {
		writes.Go(func() {
			err := store.saveFailureCapture(ctx, FailureCapture{
				RequestID: fmt.Sprint(index), CreatedAt: now, Evidence: json.RawMessage(`{}`),
			}, 3*1024)
			if err != nil {
				t.Error(err)
			}
		})
	}
	writes.Wait()
	items, err := store.ListFailureCaptures(ctx, 0, 10)
	if err != nil || len(items) != 3 || items[0].Sequence != 16 || items[2].Sequence != 14 {
		t.Fatalf("unexpected retained captures: %+v, %v", items, err)
	}
	store.Close()
	store, err = Open(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFailureCapture(ctx, items[0].RequestID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(captureRetention)
	if err := store.Prune(ctx, now, 7); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListFailureCaptures(ctx, 0, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("expired captures retained: %+v, %v", items, err)
	}
}

func TestFailureCaptureReportsStorageDegradation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	capture := FailureCapture{RequestID: "failed-request", CreatedAt: time.Now(), Evidence: json.RawMessage(`{}`)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SaveFailureCapture(ctx, capture); err == nil || !store.FailureCaptureDegraded() {
		t.Fatalf("failed write should set degradation: %v", err)
	}
	if err := store.SaveFailureCapture(context.Background(), capture); err != nil || store.FailureCaptureDegraded() {
		t.Fatalf("successful write should clear degradation: %v", err)
	}
}
