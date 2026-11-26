package failurecapture

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const exchangeLimit = 8
const entityBudget = 48 << 10

type Exchange struct {
	Method              string `json:"method"`
	Origin              string `json:"upstream_origin,omitempty"`
	Protocol            string `json:"protocol,omitempty"`
	ElapsedMillis       int64  `json:"elapsed_ms"`
	ParsedErrorType     string `json:"parsed_error_type,omitempty"`
	started             time.Time
	Attempt             int               `json:"attempt"`
	RequestHeaders      map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders     map[string]string `json:"response_headers,omitempty"`
	Stage               string            `json:"stage"`
	Request             *Body             `json:"request,omitempty"`
	Response            *Body             `json:"response,omitempty"`
	RequestUnavailable  bool              `json:"request_unavailable,omitempty"`
	ResponseUnavailable bool              `json:"response_unavailable,omitempty"`
	Status              int               `json:"status,omitempty"`
}

type Collector struct {
	mu        sync.Mutex
	exchanges []*Exchange
	truncated bool
}

type collectorKey struct{}

func WithCollector(ctx context.Context, collector *Collector) context.Context {
	return context.WithValue(ctx, collectorKey{}, collector)
}

type attemptKey struct{}

func WithAttempt(ctx context.Context, index int) context.Context {
	return context.WithValue(ctx, attemptKey{}, index)
}

func captureAttempt(ctx context.Context) int {
	index, _ := ctx.Value(attemptKey{}).(int)
	return max(1, index)
}

type originKey struct{}

func WithUpstreamOrigin(ctx context.Context, endpoint string) context.Context {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, originKey{}, safeOrigin(parsed))
}

func safeOrigin(parsed *url.URL) string {
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || len(parsed.Host) > 253 {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// DoJSON records the exact encoded body supplied to an outbound HTTP call.
// Without a collector the request follows the regular client path.
func DoJSON(client *http.Client, req *http.Request, stage string, encoded []byte) (*http.Response, error) {
	collector, _ := req.Context().Value(collectorKey{}).(*Collector)
	if collector == nil {
		return client.Do(req)
	}
	collector.mu.Lock()
	if len(collector.exchanges) >= exchangeLimit {
		collector.truncated = true
		collector.mu.Unlock()
		return client.Do(req)
	}
	exchange := &Exchange{Stage: stage, Method: "POST", Protocol: "json", Origin: safeOrigin(req.URL), started: time.Now(), Attempt: captureAttempt(req.Context()), RequestHeaders: ProtocolHeaders(req.Header), Request: NewBody(req.Header.Get("Content-Type"), entityBudget)}
	collector.exchanges = append(collector.exchanges, exchange)
	collector.mu.Unlock()

	exchange.Request.Feed(encoded)
	exchange.Request.Finish("complete")
	resp, err := client.Do(req)
	if err == nil {
		exchange.Status = resp.StatusCode
		exchange.ResponseHeaders = ProtocolHeaders(resp.Header)
		exchange.Response = NewBody(resp.Header.Get("Content-Type"), entityBudget)
		resp.Body = Observe(resp.Body, exchange.Response)
	}
	return resp, err
}

func (c *Collector) Evidence() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	exchanges := make([]Exchange, 0, len(c.exchanges))
	for _, current := range c.exchanges {
		exchange := *current
		exchange.ResponseUnavailable = exchange.Response == nil
		if exchange.ElapsedMillis == 0 {
			exchange.ElapsedMillis = time.Since(exchange.started).Milliseconds()
		}
		exchanges = append(exchanges, exchange)
	}
	encoded, _ := json.Marshal(struct {
		Exchanges []Exchange `json:"exchanges"`
		Truncated bool       `json:"truncated,omitempty"`
	}{exchanges, c.truncated})
	return encoded
}

// BeginNative records an SDK passthrough exchange before response conversion.
func BeginNative(ctx context.Context, request []byte) func(int, string, []byte, string) {
	c, _ := ctx.Value(collectorKey{}).(*Collector)
	if c == nil {
		return func(int, string, []byte, string) {}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.exchanges) >= exchangeLimit {
		c.truncated = true
		return func(int, string, []byte, string) {}
	}
	origin, _ := ctx.Value(originKey{}).(string)
	protocol := "messages"
	if request == nil {
		protocol = "chat"
	}
	exchange := &Exchange{Stage: "generation", Method: "POST", Origin: origin, Protocol: protocol, started: time.Now(), RequestUnavailable: request == nil, Attempt: captureAttempt(ctx), Request: NewBody("application/json", entityBudget)}
	if request != nil {
		exchange.Request.Feed(request)
		exchange.Request.Finish("complete")
	}
	c.exchanges = append(c.exchanges, exchange)
	return func(status int, media string, data []byte, end string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if media == "parsed_error_type" {
			switch string(data) {
			case "rate_limit_error", "invalid_request_error", "authentication_error",
				"permission_error", "not_found_error", "overloaded_error", "api_error":
				exchange.ParsedErrorType = string(data)
			}
			return
		}

		if media == "response_headers" {
			var headers map[string]string
			if json.Unmarshal(data, &headers) == nil {
				exchange.ResponseHeaders = headers
			}
			return
		}
		if media == "request" {
			exchange.RequestUnavailable = false
			exchange.Request.Feed(data)
			exchange.Request.Finish(end)
			return
		}
		if exchange.Response == nil && status != 0 && data == nil && end == "complete" {
			exchange.Status = status
			exchange.ElapsedMillis = time.Since(exchange.started).Milliseconds()
			return
		}
		if exchange.Response == nil && status != 0 {
			exchange.Status = status
			exchange.Response = NewBody(media, entityBudget)
			protocol := "messages"
			if request == nil {
				protocol = "chat"
			}
			exchange.Response.SetStreamProtocol(protocol)
		}
		if exchange.Response != nil {
			exchange.Response.Feed(data)
			if end != "" {
				exchange.Response.Finish(end)
				exchange.ElapsedMillis = time.Since(exchange.started).Milliseconds()
			}
		}
	}
}

// ObserveNativeStream observes the decoded SSE source before semantic parsing.
func ObserveNativeStream(source io.Reader, record func(int, string, []byte, string)) io.Reader {
	return nativeReader{source, record}
}

type nativeReader struct {
	source io.Reader
	record func(int, string, []byte, string)
}

func (r nativeReader) Read(data []byte) (int, error) {
	n, err := r.source.Read(data)
	end := ""
	switch {
	case err == nil:
	case errors.Is(err, io.EOF):
		end = "eof"
	case errors.Is(err, context.Canceled):
		end = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		end = "timeout"
	default:
		end = "read_error"
	}
	r.record(200, "text/event-stream", data[:n], end)
	return n, err
}

// RecordSDKHeaders projects only safe values from the SDK's response metadata.
func RecordSDKHeaders(record func(int, string, []byte, string), source map[string]string) {
	headers := make(http.Header)
	for name, value := range source {
		headers.Set(name, value)
	}
	data, err := json.Marshal(ProtocolHeaders(headers))
	if err == nil {
		record(0, "response_headers", data, "")
	}
}
