package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

func registerFailureCaptures(service *management.Service, store *state.Store) {
	service.Register("records.capture.list", func(ctx context.Context, _ string, call management.Call) (management.Result, error) {
		var input struct {
			Before int64
			Limit  int
		}
		input.Limit = 50
		if err := json.Unmarshal(call.Input, &input); err != nil {
			return management.Result{}, err
		}
		items, err := store.ListFailureCaptures(ctx, input.Before, input.Limit)
		if err != nil {
			return management.Result{}, err
		}
		var next int64
		if len(items) == input.Limit {
			next = items[len(items)-1].Sequence
		}
		return management.Success(map[string]any{"captures": items, "next_before": next}, time.Now()), nil
	})
	service.Register("records.capture.get", func(ctx context.Context, _ string, call management.Call) (management.Result, error) {
		var input struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(call.Input, &input); err != nil {
			return management.Result{}, err
		}
		item, err := store.GetFailureCapture(ctx, input.RequestID)
		if err != nil {
			return management.Result{}, err
		}
		return management.Success(item, time.Now()), nil
	})
	service.Register("records.capture.delete", func(ctx context.Context, _ string, call management.Call) (management.Result, error) {
		var input struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(call.Input, &input); err != nil {
			return management.Result{}, err
		}
		if err := store.DeleteFailureCapture(ctx, input.RequestID); err != nil {
			return management.Result{}, err
		}
		return management.Success(map[string]bool{"deleted": true}, time.Now()), nil
	})
}
