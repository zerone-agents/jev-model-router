package failurecapture

import (
	"io"
)

type OriginalBody struct {
	Bytes []byte `json:"bytes_base64,omitempty"`
	State string `json:"state"`
}

type OriginalExchange struct {
	Stage   string       `json:"stage"`
	Attempt int          `json:"attempt"`
	Body    OriginalBody `json:"body"`
}

type Originals struct {
	Client          OriginalBody       `json:"client"`
	Upstream        []OriginalExchange `json:"upstream,omitempty"`
	UpstreamOmitted bool               `json:"upstream_omitted,omitempty"`
}

type OriginalReader struct {
	io.ReadCloser
	original OriginalBody
	limit    int64
}

func ObserveOriginal(source io.ReadCloser, limit int64) *OriginalReader {
	return &OriginalReader{ReadCloser: source, limit: limit, original: OriginalBody{State: "incomplete"}}
}

func (r *OriginalReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if r.original.State == "incomplete" {
		if int64(n)+int64(len(r.original.Bytes)) > r.limit {
			r.original.State = "oversized"
			r.original.Bytes = nil
		} else {
			r.original.Bytes = append(r.original.Bytes, p[:n]...)
			if err == io.EOF {
				r.original.State = "complete"
			}
		}
	}
	return n, err
}

func (r *OriginalReader) Result() OriginalBody { return r.original }

func (c *Collector) OriginalRequests() ([]OriginalExchange, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	items := make([]OriginalExchange, 0, len(c.exchanges))
	for _, exchange := range c.exchanges {
		body := OriginalBody{State: "unavailable"}
		if exchange.original != nil {
			body = OriginalBody{State: "complete", Bytes: exchange.original}
		}
		items = append(items, OriginalExchange{Stage: exchange.Stage, Attempt: exchange.Attempt, Body: body})
	}
	return items, c.truncated
}
