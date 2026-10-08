package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"testing"
	"time"
)

func TestNativeRegressionParallelTrue(t *testing.T) {
	r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"parallel_tool_calls":true,"reasoning_effort":"none"}`)
	if err := Check(nativeTarget("https://example.com/v1"), r); err != nil {
		t.Fatalf("valid parallel=true rejected: %v", err)
	}
}
func TestNativeRegressionJSONSchemaDescription(t *testing.T) {
	r := req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","description":"Return all monetary amounts in cents.","strict":true,"schema":{"type":"object","properties":{"v":{"type":"integer"}},"required":["v"],"additionalProperties":false}}}}`)
	err := Check(nativeTarget("https://example.com/v1"), r)
	if err == nil || !strings.Contains(err.Error(), "unsupported_request") {
		t.Fatalf("description must be rejected during candidate checking: %v", err)
	}
}
func TestNativeRegressionEmptyToolStream(t *testing.T) {
	tool := nativeFrame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"now","input":{}}}`) + nativeFrame("content_block_stop", `{"type":"content_block_stop","index":0}`)
	events, e := collectNative(t, nativeStart()+tool+strings.Replace(nativeEnd(), "end_turn", "tool_use", 1))
	b, _ := json.Marshal(events)
	if e != nil {
		t.Fatalf("valid no-argument tool failed: %v events=%s", e, b)
	}
}

func TestNativeRegressionCancellationStatus(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		for _, closed := range []bool{false, true} {
			for i := 0; i < 40; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				want := 499
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = 504
				}
				bc := schemas.NewBifrostContext(ctx, time.Time{})
				ch := make(chan *schemas.BifrostStreamChunk, 1)
				if closed {
					close(ch)
				} else {
					ch <- &schemas.BifrostStreamChunk{BifrostError: &schemas.BifrostError{}}
				}
				state := &anthropicUpstreamValidation{failure: nativeError(502)}
				released := false
				stream := &anthropicUpstreamStream{ctx: bc, ch: ch, state: state, release: func() { released = true }}
				cancel()
				_, err := stream.Next(ctx)
				var upstream *routing.UpstreamError
				if !errors.As(err, &upstream) || upstream.Status != want {
					t.Fatalf("deadline=%v closed=%v: got %v; want %d", deadline, closed, err, want)
				}
				if !released {
					t.Fatal("lease not released")
				}
			}
		}
	}
}
func TestNativeRegressionInitialSSEError(t *testing.T) {
	for _, tc := range []struct {
		typ    string
		status int
	}{{"overloaded_error", 529}, {"rate_limit_error", 429}} {
		_, e := collectNative(t, nativeFrame("error", `{"type":"error","error":{"type":"`+tc.typ+`","message":"private"}}`))
		var upstream *routing.UpstreamError
		if !errors.As(e, &upstream) || upstream.Status != tc.status {
			t.Errorf("%s: got %v (%+v); want status %d", tc.typ, e, upstream.Status, tc.status)
		}
	}
}
