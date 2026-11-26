package httptransport

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

type captureTestSink struct{ saved []state.FailureCapture }

func (s *captureTestSink) SaveFailureCapture(_ context.Context, capture state.FailureCapture) error {
	s.saved = append(s.saved, capture)
	return nil
}

func TestFailureCaptureCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"client cancellation", routing.Fail("cancelled", "request cancelled"), 0},
		{"failure before cancellation", routing.Fail("upstream_error", "upstream failed"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
			sink := &captureTestSink{}
			_, _, finish := beginFailureCapture(httptest.NewRecorder(), req, sink)
			cancel()
			finish(tc.err)
			if len(sink.saved) != tc.want {
				t.Fatalf("saved %d captures, want %d", len(sink.saved), tc.want)
			}
		})
	}
}

func TestSaturatedCaptureKeepsTerminalError(t *testing.T) {
	for range cap(captureSlots) {
		captureSlots <- struct{}{}
	}
	defer func() {
		for range cap(captureSlots) {
			<-captureSlots
		}
	}()
	sink := &captureTestSink{}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("original request bytes"))
	writer, captured, finish := beginFailureCapture(httptest.NewRecorder(), req, sink)
	if captured.Body != req.Body || captured.Context() != req.Context() {
		t.Fatal("saturated capture installed a body reader or upstream collector")
	}
	if _, err := io.Copy(io.Discard, captured.Body); err != nil {
		t.Fatal(err)
	}
	writer.WriteHeader(200)
	finish(&routing.UpstreamError{Status: 429})
	if len(sink.saved) != 1 || sink.saved[0].ErrorCode != "upstream_rate_limit" {
		t.Fatalf("missing terminal classification: %+v", sink.saved)
	}
	originals := sink.saved[0].Originals
	if originals == nil || originals.Client.State != "omitted" || len(originals.Client.Bytes) != 0 || !originals.UpstreamOmitted {
		t.Fatalf("missing omission markers: %+v", originals)
	}
}
