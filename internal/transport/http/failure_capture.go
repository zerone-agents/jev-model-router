package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/failurecapture"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"github.com/zerone-agents/jev-model-router/internal/state"
)

type failureCaptureSink interface {
	SaveFailureCapture(context.Context, state.FailureCapture) error
}

// Bound concurrent payload collection; saturated requests retain metadata.
var captureSlots = make(chan struct{}, 32)

type captureWriter struct {
	http.ResponseWriter
	body        *failurecapture.Body
	status      int
	collectBody bool
	protocol    string
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *captureWriter) Flush()                      { _ = w.FlushError() }

func (w *captureWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil && w.body != nil {
		w.body.Finish("write_error")
	}
	return err
}

func (w *captureWriter) WriteHeader(status int) {
	if w.status == 0 && status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(data []byte) (int, error) {
	if w.collectBody && w.body == nil {
		w.body = failurecapture.NewBody(w.Header().Get("Content-Type"), 48<<10)
		w.body.SetStreamProtocol(w.protocol)
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	if w.body != nil {
		w.body.Feed(data[:n])
		if err != nil {
			w.body.Finish("write_error")
		}
	}
	return n, err
}

func beginFailureCapture(w http.ResponseWriter, r *http.Request, store any) (http.ResponseWriter, *http.Request, func(error)) {
	sink, ok := store.(failureCaptureSink)
	if !ok {
		return w, r, func(error) {}
	}
	ctx := r.Context()
	protocol := "unknown"
	switch r.URL.Path {
	case "/v1/chat/completions":
		protocol = "chat"
	case "/v1/messages":
		protocol = "messages"
	case "/admin/v1/playground/completions":
		protocol = "playground"
	}
	requestHeaders := failurecapture.ProtocolHeaders(r.Header)
	start := time.Now()
	id := w.Header().Get("X-Request-ID")
	var request *failurecapture.Body
	var collector *failurecapture.Collector
	writer := &captureWriter{ResponseWriter: w, protocol: protocol}
	select {
	case captureSlots <- struct{}{}:
		writer.collectBody = true
		collector = &failurecapture.Collector{}
		request = failurecapture.NewBody(r.Header.Get("Content-Type"), 48<<10)
		r = r.WithContext(failurecapture.WithCollector(ctx, collector))
		r.Body = failurecapture.Observe(r.Body, request)
	default:
	}
	return writer, r, func(finalErr error) {
		if writer.collectBody {
			defer func() { <-captureSlots }()
		}
		if finalErr == nil || errors.Is(finalErr, context.Canceled) {
			return
		}
		var failure *routing.Error
		if errors.Is(ctx.Err(), context.Canceled) && errors.As(finalErr, &failure) && failure.Code == "cancelled" {
			return
		}
		var upstream json.RawMessage
		if request != nil {
			request.Finish("closed")
			upstream = collector.Evidence()
		}
		if writer.body != nil {
			end := "complete"
			if writer.body.SSE != nil {
				end = "closed"
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				end = "canceled"
			} else if errors.Is(finalErr, context.DeadlineExceeded) {
				end = "timeout"
			}
			writer.body.Finish(end)
		}
		evidence := map[string]any{
			"request": request, "response": writer.body, "status": writer.status, "upstream": upstream, "payload_omitted": !writer.collectBody,
			"method": "POST", "protocol": protocol, "duration_ms": time.Since(start).Milliseconds(),
			"request_headers": requestHeaders, "response_headers": failurecapture.ProtocolHeaders(writer.Header()),
		}
		if errors.As(finalErr, &failure) && failure.ChatValidation != nil {
			evidence["chat_validation"] = failure.ChatValidation
		}
		encoded, err := json.Marshal(evidence)
		if err != nil {
			return
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = sink.SaveFailureCapture(saveCtx, state.FailureCapture{ErrorCode: routing.Diagnose(finalErr).Code, RequestID: id, CreatedAt: time.Now(), Evidence: encoded})
	}
}
