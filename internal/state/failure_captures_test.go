package state

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/failurecapture"
	"path/filepath"
	"strings"
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

func TestOriginalCaptureEncryptedAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	raw := []byte(" byte-exact confidential request \x00\n")
	capture := FailureCapture{RequestID: "example", CreatedAt: time.Now(), Evidence: json.RawMessage(`{}`),
		Originals: &failurecapture.Originals{Client: failurecapture.OriginalBody{Bytes: raw, State: "complete"}}}
	if err := store.SaveFailureCapture(ctx, capture); err != nil {
		t.Fatal(err)
	}
	var row string
	if err := store.db.QueryRow("SELECT body FROM failure_captures").Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, "bytes_base64") || !strings.Contains(row, "encrypted_requests") {
		t.Fatal("original stored outside ciphertext")
	}
	store.Close()
	store, err = Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetFailureCapture(ctx, capture.RequestID)
	if err != nil || got.Originals == nil || string(got.Originals.Client.Bytes) != string(raw) {
		t.Fatalf("original unavailable after restart: %v", err)
	}
}
