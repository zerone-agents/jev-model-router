package failurecapture

import (
	"context"
	"errors"
	"io"
	"mime"
)

// Body accumulates a bounded representation of an HTTP entity. The raw
// buffer is private and released when the entity terminates.
type Body struct {
	JSON      *Projection `json:"json,omitempty"`
	SSE       *Stream     `json:"sse,omitempty"`
	Omitted   bool        `json:"omitted,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	End       string      `json:"termination,omitempty"`

	buffer    []byte
	remaining int
	parseJSON bool
}

func NewBody(contentType string, budget int) *Body {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "text/event-stream":
		return &Body{SSE: NewStream(budget)}
	case "application/json":
		return &Body{parseJSON: true, remaining: max(0, budget)}
	default:
		return &Body{Omitted: true}
	}
}

func (b *Body) SetStreamProtocol(protocol string) {
	if b.SSE != nil {
		b.SSE.protocol = protocol
	}
}

func (b *Body) Feed(data []byte) {
	if b.End != "" {
		return
	}
	if b.SSE != nil {
		b.SSE.Feed(data)
		return
	}
	if !b.parseJSON || b.Truncated {
		return
	}
	if len(data) > b.remaining {
		b.buffer = nil
		b.Truncated = true
		b.Omitted = true
		return
	}
	b.remaining -= len(data)
	b.buffer = append(b.buffer, data...)
}

func (b *Body) Finish(reason string) {
	if b.End != "" {
		return
	}
	switch reason {
	case "eof", "complete", "closed", "read_error", "write_error", "timeout", "canceled":
		b.End = reason
	default:
		b.End = "unknown"
	}
	defer func() { b.buffer = nil }()
	if b.SSE != nil {
		b.SSE.Finish(b.End)
		return
	}
	if b.parseJSON && !b.Truncated {
		if b.End != "eof" && b.End != "complete" {
			b.Omitted = true
			return
		}
		projected := ProjectJSON(b.buffer)
		b.JSON = &projected
	}
}

// Observe reads the entity exactly as the caller consumes it.
func Observe(source io.ReadCloser, capture *Body) io.ReadCloser {
	return &observedBody{source: source, capture: capture}
}

type observedBody struct {
	source  io.ReadCloser
	capture *Body
}

func (r *observedBody) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.capture.Feed(p[:n])
	switch {
	case err == nil:
	case errors.Is(err, io.EOF):
		r.capture.Finish("eof")
	case errors.Is(err, context.Canceled):
		r.capture.Finish("canceled")
	case errors.Is(err, context.DeadlineExceeded):
		r.capture.Finish("timeout")
	default:
		r.capture.Finish("read_error")
	}
	return n, err
}

func (r *observedBody) Close() error {
	err := r.source.Close()
	r.capture.Finish("closed")
	return err
}
