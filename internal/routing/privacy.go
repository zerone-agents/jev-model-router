package routing

import (
	"context"
	"encoding/json"
)

type PrivacyOutcome string

const (
	PrivacyNonSensitive PrivacyOutcome = "non_sensitive"
	PrivacySensitive    PrivacyOutcome = "sensitive"
	PrivacyUncertain    PrivacyOutcome = "uncertain"
)

type PrivacyDecision struct {
	Outcome PrivacyOutcome
	Usage   *Usage
	Invoked bool
}
type PrivacyClassifier interface {
	Classify(context.Context, DecisionConfig, string, json.RawMessage) (PrivacyDecision, error)
}

// AssessPrivacy normalizes classifier failures to uncertain without exposing raw
// provider errors. Invocation and token accounting remain separate from outcome.
type PrivacyAssessment struct {
	Outcome PrivacyOutcome
	Usage   DecisionUsage
	Failed  bool
}

func AssessPrivacy(ctx context.Context, snapshot Snapshot, classifier PrivacyClassifier, raw json.RawMessage) PrivacyAssessment {
	out := PrivacyAssessment{Outcome: PrivacyUncertain, Usage: DecisionUsage{Complete: true}}
	if snapshot.Decision.Location != "local" || snapshot.Decision.Model == "" || snapshot.Decision.BaseURL == "" || classifier == nil {
		out.Failed = true
		return out
	}
	result, err := classifier.Classify(ctx, snapshot.Decision, snapshot.Privacy.Prompt, raw)
	if result.Invoked {
		out.Usage.Invocations = 1
		if result.Usage == nil || result.Usage.InputTokens < 0 || result.Usage.OutputTokens < 0 || err != nil {
			out.Usage.Complete = false
		}
		if result.Usage != nil && result.Usage.InputTokens >= 0 && result.Usage.OutputTokens >= 0 {
			out.Usage.InputTokens = result.Usage.InputTokens
			out.Usage.OutputTokens = result.Usage.OutputTokens
		}
	}
	if err != nil || ctx.Err() != nil {
		out.Failed = true
		if result.Invoked {
			out.Usage.Complete = false
		}
		return out
	}
	if !result.Invoked && result.Outcome == PrivacyNonSensitive {
		out.Failed = true
		return out
	}
	switch result.Outcome {
	case PrivacyNonSensitive, PrivacySensitive, PrivacyUncertain:
		out.Outcome = result.Outcome
	default:
		out.Failed = true
		if result.Invoked {
			out.Usage.Complete = false
		}
	}
	return out
}
