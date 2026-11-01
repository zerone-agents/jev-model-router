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
func TestNativeRegressionSSEErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		typ    string
		status int
	}{
		{"invalid_request_error", 400}, {"authentication_error", 401}, {"billing_error", 402}, {"permission_error", 403}, {"not_found_error", 404}, {"conflict_error", 409}, {"request_too_large", 413}, {"rate_limit_error", 429}, {"api_error", 500}, {"timeout_error", 504}, {"overloaded_error", 529}, {"unknown_error", 502},
	} {
		for _, phase := range []string{"initial", "midstream"} {
			t.Run(tc.typ+"/"+phase, func(t *testing.T) {
				prefix := ""
				if phase == "midstream" {
					prefix = nativeStart() + nativeText()
				}
				events, err := collectNative(t, prefix+nativeFrame("error", `{"type":"error","error":{"type":"`+tc.typ+`","message":"private diagnostic"}}`))
				var got *routing.UpstreamError
				want := nativeError(tc.status).(*routing.UpstreamError)
				if !errors.As(err, &got) {
					t.Fatalf("expected upstream error, got %v", err)
				}
				wantCode := tc.typ
				if tc.typ == "unknown_error" {
					wantCode = ""
				}
				if d := routing.Diagnose(err); d.UpstreamCode != wantCode {
					t.Fatalf("reported code: %+v", d)
				}
				actual, _ := json.Marshal(got.Body)
				expected, _ := json.Marshal(want.Body)
				if got.Status != want.Status || string(actual) != string(expected) {
					t.Fatalf("got %d %s; want %d %s", got.Status, actual, want.Status, expected)
				}
				for _, event := range events {
					for _, choice := range event.Choices {
						if choice.FinishReason != nil {
							t.Fatal("error stream emitted successful finish")
						}
					}
				}
			})
		}
	}
}

func TestBigModel1301Classification(t *testing.T) {
	err := nativeReportedError(400, `{"error":{"code":"1301","type":"invalid_request_error","message":"[1301] Safety policy rejected content."}}`)
	got := routing.Diagnose(err)
	if got.Code != "upstream_content_rejected" || got.UpstreamCode != "content_filter" {
		t.Fatalf("unexpected diagnostic: %+v", got)
	}
}

func TestBigModel1301StreamAndJSON(t *testing.T) {
	raw := `{"type":"error","error":{"code":"1301","type":"invalid_request_error","message":"[1301][内容安全] Safety policy rejected content. Authorization: Bearer SECRET"}}`
	for name, call := range map[string]func() error{
		"json": func() error { return nativeReportedError(400, raw, "SECRET") },
		"sse":  func() error { _, err := collectNative(t, nativeFrame("error", raw)); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			var up *routing.UpstreamError
			if !errors.As(err, &up) {
				t.Fatalf("not upstream: %v", err)
			}
			d := routing.Diagnose(err)
			if d.Code != "upstream_content_rejected" || up.Body["code"] != "content_filter" {
				t.Fatalf("classification: %+v", d)
			}
			b, _ := json.Marshal(up)
			if strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), "Safety policy rejected content") {
				t.Fatalf("redaction or message: %s", b)
			}
		})
	}
}

func TestOversizedSSEErrorPreservesReportedType(t *testing.T) {
	raw := `{"type":"error","error":{"type":"insufficient_quota","message":"` + strings.Repeat("x", 70<<10) + `"}}`
	for name, call := range map[string]func() error{
		"chat":     func() error { _, err := collectNative(t, nativeFrame("error", raw)); return err },
		"messages": func() error { _, err := readNativeFixture(t, nativeFrame("error", raw)); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			var up *routing.UpstreamError
			if !errors.As(err, &up) {
				t.Fatalf("expected upstream error: %v", err)
			}
			d := routing.Diagnose(err)
			if d.Code != "upstream_quota" || d.UpstreamCode != "insufficient_quota" {
				t.Fatalf("lost classification: %+v", d)
			}
			if up.Details == nil || !up.Details.Truncated {
				t.Fatal("oversized body not omitted")
			}
		})
	}
}
