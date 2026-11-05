package inference

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type privacyClassifierFixture struct {
	calls   int
	outcome routing.PrivacyOutcome
	err     error
	raw     json.RawMessage
}

func (p *privacyClassifierFixture) Classify(_ context.Context, _ routing.DecisionConfig, _ string, raw json.RawMessage) (routing.PrivacyDecision, error) {
	p.calls++
	p.raw = append([]byte(nil), raw...)
	return routing.PrivacyDecision{Outcome: p.outcome, Invoked: true, Usage: &routing.Usage{InputTokens: 5, OutputTokens: 1}}, p.err
}
func TestPrivacyExecutionFiltersAndReusesClassification(t *testing.T) {
	classifier := &privacyClassifierFixture{outcome: routing.PrivacySensitive}
	snap := routing.Snapshot{Privacy: routing.PrivacyConfig{Enabled: true, Prompt: "classify"}, Decision: routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}, Providers: []routing.Provider{{ID: "p"}}, Models: []routing.Model{
		{ID: "local-a", ProviderID: "p", Location: "local", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}},
		{ID: "local-b", ProviderID: "p", Location: "local", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}},
		{ID: "cloud", ProviderID: "p", Location: "cloud", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}},
	}}
	planner := &routing.Planner{PrivacyClassifier: classifier, Decider: &usageDecider{}}
	svc := &Service{Store: usageStore{snap}, Planner: planner, DecisionTimeout: time.Second, Fallback: DefaultFallbackConfig()}
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"secret"`)}}}
	r := svc.Begin(context.Background(), "privacy")
	calls := 0
	err := r.Execute(req, nil, nil, "", func(_ context.Context, p routing.Plan, n int) AttemptResult {
		calls++
		if p.Target.Model.Location != "local" {
			t.Fatal("cloud selected")
		}
		if n == 1 {
			return AttemptResult{Err: routing.Fail("timeout", "failed"), RetryableGenerationFailure: true}
		}
		return AttemptResult{}
	})
	if err != nil || calls != 2 || classifier.calls != 1 {
		t.Fatalf("calls=%d classifier=%d err=%v", calls, classifier.calls, err)
	}
	if len(r.record.Attempts) != 2 || r.record.DecisionUsage.Invocations != 2 {
		t.Fatalf("accounting: %+v %+v", r.record.DecisionUsage, r.record.Attempts)
	}
}
func TestPrivacyExecutionRejectsExplicitCloudWhenClassificationFails(t *testing.T) {
	classifier := &privacyClassifierFixture{outcome: routing.PrivacyUncertain, err: errors.New("failure")}
	snap := routing.Snapshot{Privacy: routing.PrivacyConfig{Enabled: true, Prompt: "classify"}, Decision: routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}, Providers: []routing.Provider{{ID: "p"}}, Models: []routing.Model{{ID: "cloud", ProviderID: "p", Location: "cloud", Enabled: true, Capabilities: routing.Capabilities{ContextLimit: 10000}}}}
	svc := &Service{Store: usageStore{snap}, Planner: &routing.Planner{PrivacyClassifier: classifier}, DecisionTimeout: time.Second}
	req := routing.Request{Model: "cloud", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"secret"`)}}}
	r := svc.Begin(context.Background(), "privacy")
	err := r.Execute(req, nil, nil, "", func(context.Context, routing.Plan, int) AttemptResult {
		t.Fatal("generated on cloud")
		return AttemptResult{}
	})
	if err == nil || classifier.calls != 1 || r.record.DecisionUsage.Complete {
		t.Fatalf("err=%v calls=%d usage=%+v", err, classifier.calls, r.record.DecisionUsage)
	}
}

type cancellingPrivacyClassifier struct{ cancel context.CancelFunc }

func (c cancellingPrivacyClassifier) Classify(context.Context, routing.DecisionConfig, string, json.RawMessage) (routing.PrivacyDecision, error) {
	c.cancel()
	return routing.PrivacyDecision{Outcome: routing.PrivacyUncertain, Invoked: true}, context.Canceled
}
func TestPrivacyCancellationRecordsFirstAttemptUsage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	snap := routing.Snapshot{Privacy: routing.PrivacyConfig{Enabled: true, Prompt: "classify"}, Decision: routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}}
	svc := &Service{Store: usageStore{snap}, Planner: &routing.Planner{PrivacyClassifier: cancellingPrivacyClassifier{cancel: cancel}}, DecisionTimeout: time.Second}
	r := svc.Begin(ctx, "cancel")
	req := routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"secret"`)}}}
	err := r.Execute(req, nil, nil, "", func(context.Context, routing.Plan, int) AttemptResult {
		t.Fatal("unexpected generation")
		return AttemptResult{}
	})
	if err == nil || len(r.record.Attempts) != 1 || r.record.Attempts[0].DecisionUsage.Invocations != 1 || r.record.DecisionUsage.Complete {
		t.Fatalf("err=%v attempts=%+v usage=%+v", err, r.record.Attempts, r.record.DecisionUsage)
	}
}

type invalidPrivacyClassifier struct{ result routing.PrivacyDecision }

func (c invalidPrivacyClassifier) Classify(context.Context, routing.DecisionConfig, string, json.RawMessage) (routing.PrivacyDecision, error) {
	return c.result, nil
}
func TestPrivacyAssessmentRejectsInvalidOrUninvokedResults(t *testing.T) {
	snap := routing.Snapshot{Privacy: routing.PrivacyConfig{Enabled: true, Prompt: "classify"}, Decision: routing.DecisionConfig{Location: "local", Model: "local", BaseURL: "http://127.0.0.1"}}
	for _, tc := range []struct {
		result     routing.PrivacyDecision
		incomplete bool
	}{
		{routing.PrivacyDecision{Outcome: "invalid", Invoked: true, Usage: &routing.Usage{InputTokens: 4, OutputTokens: 1}}, true},
		{routing.PrivacyDecision{Outcome: routing.PrivacyNonSensitive, Invoked: false}, false},
	} {
		got := routing.AssessPrivacy(context.Background(), snap, invalidPrivacyClassifier{tc.result}, json.RawMessage(`{}`))
		if got.Outcome != routing.PrivacyUncertain || !got.Failed || got.Usage.Complete == tc.incomplete {
			t.Fatalf("result: %+v", got)
		}
	}
}
