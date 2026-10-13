package routing

// DecisionUsage contains known token counts only. Incomplete totals are lower
// bounds, never evidence of a free decision. Invocations counts adapter entries,
// including failures before transmission; it is not a billable request count.
type DecisionUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Invocations  int   `json:"invocations"`
	Complete     bool  `json:"complete"`
}

func (p Plan) RecordedDecisionUsage() *DecisionUsage {
	u := &DecisionUsage{Complete: true}
	if !p.DecisionInvoked {
		return u
	}
	u.Invocations = 1
	if p.DecisionUsage == nil || p.DecisionUsage.InputTokens < 0 || p.DecisionUsage.OutputTokens < 0 {
		u.Complete = false
		return u
	}
	u.InputTokens = p.DecisionUsage.InputTokens
	u.OutputTokens = p.DecisionUsage.OutputTokens
	return u
}
