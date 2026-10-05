package httptransport

import (
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http/httptest"
	"testing"
)

func TestUpstreamErrorResponse(t *testing.T) {
	body := map[string]any{"message": "quota exceeded", "type": "rate_limit_error", "code": "quota", "param": "model"}
	err := fmt.Errorf("wrapped: %w", &routing.UpstreamError{Status: 429, Body: body})
	w := httptest.NewRecorder()
	writeError(w, err)
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
	var got map[string]map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	for key, value := range body {
		if got["error"][key] != value {
			t.Fatalf("field %s: %v", key, got)
		}
	}
	if errorBody(err)["error"].(map[string]any)["message"] != "quota exceeded" {
		t.Fatal("SSE error lost")
	}
	if management.Failure(err).Error.Message != "generation provider failed" {
		t.Fatal("provider details leaked into management")
	}
}
