package routing

import (
	"context"
	"github.com/zerone-agents/jev-model-router/internal/health"
	"testing"
)

func TestHealthClassification(t *testing.T) {
	for _, tt := range []struct {
		e error
		k health.Kind
	}{{context.Canceled, ""}, {ErrGenerationCapacity, ""}, {&UpstreamError{Status: 403}, ""}, {&UpstreamError{Status: 451}, ""}, {&UpstreamError{Status: 429}, health.Limited}, {&UpstreamError{Status: 401, ReportedCode: "invalid_api_key"}, health.Permanent}, {&UpstreamError{Status: 500}, health.Transient}} {
		if got := HealthOutcome(tt.e); got.Kind != tt.k {
			t.Fatalf("%v: %s", tt.e, got.Kind)
		}
	}
}
