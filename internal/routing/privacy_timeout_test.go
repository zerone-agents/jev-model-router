package routing

import (
	"context"
	"encoding/json"
	"testing"
)

type latePrivacyClassifier struct{}

func (latePrivacyClassifier) Classify(ctx context.Context, _ DecisionConfig, _ string, _ json.RawMessage) (PrivacyDecision, error) {
	<-ctx.Done()
	return PrivacyDecision{Outcome: PrivacyNonSensitive, Invoked: true, Usage: &Usage{InputTokens: 1}}, nil
}

func TestPrivacyLateResultCannotPermitCloud(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snapshot := Snapshot{Decision: DecisionConfig{Location: "local", Model: "model", BaseURL: "http://localhost"}}
	got := AssessPrivacy(ctx, snapshot, latePrivacyClassifier{}, json.RawMessage(`{}`))
	if got.Outcome != PrivacyUncertain || !got.Failed || got.Usage.Complete || got.Usage.Invocations != 1 {
		t.Fatalf("late result was accepted: %+v", got)
	}
}
