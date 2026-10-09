package provider

import (
	"context"
	"encoding/json"
	"errors"
	bifrost "github.com/maximhq/bifrost/core"
	u "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type probeAccount struct{ fixedAccount }

func (a probeAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{schemas.Anthropic}, nil
}
func (a probeAccount) GetConfigForProvider(schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	return &schemas.ProviderConfig{NetworkConfig: schemas.NetworkConfig{BaseURL: a.url, AllowPrivateNetwork: true, DefaultRequestTimeoutInSeconds: 5}, ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{Concurrency: 1, BufferSize: 2}}, nil
}

type probeReader struct {
	inner    u.SSEEventReader
	verified bool
	rejected bool
}

func (r *probeReader) ReadEvent() (string, []byte, error) {
	typ, b, e := r.inner.ReadEvent()
	if e != nil {
		return typ, b, e
	}
	if !json.Valid(b) {
		r.rejected = true
		return "", nil, errors.New("invalid native event")
	}
	if typ == "message_stop" {
		_, _, e = r.inner.ReadEvent()
		if e != io.EOF {
			r.rejected = true
			return "", nil, errors.New("event after terminal")
		}
		r.verified = true
	}
	return typ, b, nil
}
func TestCaptureBoundaryProbe(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"
	stop := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, tc := range []struct {
		name, body string
		bad        bool
	}{{"malformed_then_stop", start + "event: content_block_delta\ndata: {bad\n\n" + stop, true}, {"error_after_stop", start + stop + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}}\n\n", true}, {"clean", start + stop, false}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c, e := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: probeAccount{fixedAccount{url: srv.URL, key: "test"}}, Logger: quietLogger{}, InitialPoolSize: 1})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Shutdown()
			bc := schemas.NewBifrostContext(context.Background(), time.Now().Add(5*time.Second))
			defer bc.Cancel()
			var captured *probeReader
			bc.SetValue(schemas.BifrostContextKeySSEReaderFactory, &u.SSEReaderFactory{NewEventReader: func(rd io.Reader) u.SSEEventReader {
				captured = &probeReader{inner: u.GetSSEEventReader(nil, rd)}
				return captured
			}})
			request, e := encode(bc, target(srv.URL), req(t, `{"model":"fast","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`))
			if e != nil {
				t.Fatal(e)
			}
			request.Provider = schemas.Anthropic
			request.Model = "claude-test"
			ch, fail := c.ChatCompletionStreamRequest(bc, request)
			if fail != nil {
				t.Fatalf("startup: %v", fail)
			}
			failed, finished := false, false
			for v := range ch {
				if v.BifrostError != nil {
					failed = true
				}
				if v.BifrostChatResponse != nil {
					for _, choice := range v.BifrostChatResponse.Choices {
						if choice.FinishReason != nil {
							finished = true
						}
					}
				}
			}
			if captured == nil {
				t.Fatal("factory not invoked")
			}
			if tc.bad && (!captured.rejected || !failed || finished) {
				t.Fatalf("rejected=%v failed=%v finished=%v", captured.rejected, failed, finished)
			}
			if !tc.bad && (!captured.verified || failed) {
				t.Fatalf("verified=%v failed=%v", captured.verified, failed)
			}
		})
	}
}
