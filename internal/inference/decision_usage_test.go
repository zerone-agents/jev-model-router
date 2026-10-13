package inference

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
	"path/filepath"
	"testing"
	"time"
)

type usageStore struct{ snapshot routing.Snapshot }

func (s usageStore) Snapshot(context.Context) (routing.Snapshot, error) { return s.snapshot, nil }

type usageDecider struct {
	calls   int
	missing bool
	fail    bool
}

func (d *usageDecider) Choose(_ context.Context, _ routing.DecisionConfig, in routing.DecisionInput) (routing.Decision, error) {
	d.calls++
	out := routing.Decision{ModelID: in.Candidates[0].ID, Usage: &routing.Usage{InputTokens: 12, OutputTokens: 3}}
	if d.missing && d.calls == 1 {
		out.Usage = nil
	}
	if d.fail {
		return out, routing.Fail("invalid_decision", "invalid choice")
	}
	return out, nil
}
func TestDecisionUsagePersistence(t *testing.T) {
	for _, mode := range []string{"initial", "reselection", "missing", "failed", "explicit", "single"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			db, err := state.Open(filepath.Join(t.TempDir(), "records.db"), time.Now)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			// Old records must remain unknown, rather than gaining synthetic zero usage.
			if err := db.Append(ctx, routing.Record{RequestID: "old", CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			snap, _ := (&snapshotCounter{}).Snapshot(ctx)
			third := snap.Models[0]
			third.ID = "c"
			snap.Models = append(snap.Models, third)
			if mode == "single" {
				snap.Models = snap.Models[:1]
			}
			d := &usageDecider{missing: mode == "missing", fail: mode == "failed"}
			svc := &Service{Store: usageStore{snap}, Planner: &routing.Planner{Decider: d}, Recorder: &routing.Recorder{Sink: db}, DecisionTimeout: time.Second, Fallback: DefaultFallbackConfig()}
			r := svc.Begin(ctx, "new")
			req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
			if mode == "explicit" {
				req.Model = "a"
			}
			err = r.Execute(req, nil, nil, "hi", func(_ context.Context, _ routing.Plan, n int) AttemptResult {
				if n == 1 && (mode == "reselection" || mode == "missing") {
					return AttemptResult{Err: routing.Fail("timeout", "slow"), RetryableGenerationFailure: true}
				}
				return AttemptResult{}
			})
			r.Finish(err)
			if (err != nil) != (mode == "failed") {
				t.Fatalf("error: %v", err)
			}
			page, err := db.ListRecords(ctx, "", 10)
			if err != nil || len(page.Records) != 2 {
				t.Fatalf("%+v %v", page, err)
			}
			if page.Records[1].DecisionUsage != nil {
				t.Fatal("historical usage fabricated")
			}
			got := page.Records[0]
			u := got.DecisionUsage
			calls := 1
			if mode == "reselection" || mode == "missing" {
				calls = 2
			}
			if mode == "single" || mode == "explicit" {
				calls = 0
			}
			known := calls
			if mode == "missing" {
				known--
			}
			if u == nil || u.Invocations != calls || u.InputTokens != int64(12*known) || u.OutputTokens != int64(3*known) || u.Complete != (mode != "missing") {
				t.Fatalf("usage: %+v", u)
			}
			var input, output int64
			for _, a := range got.Attempts {
				if a.DecisionUsage == nil {
					t.Fatal("missing attempt usage")
				}
				input += a.DecisionUsage.InputTokens
				output += a.DecisionUsage.OutputTokens
			}
			if input != u.InputTokens || output != u.OutputTokens {
				t.Fatal("attempt totals disagree")
			}
		})
	}
}
