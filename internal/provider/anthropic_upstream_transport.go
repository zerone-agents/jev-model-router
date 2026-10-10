package provider

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// Use the SDK's public native transport so the exact body checked during
// candidate preparation is sent without another lossy Chat serialization.
func nativePassthroughRequest(req *schemas.BifrostChatRequest) *schemas.BifrostPassthroughRequest {
	return &schemas.BifrostPassthroughRequest{Model: req.Model, Method: "POST", Path: "/messages", Body: req.RawRequestBody, SafeHeaders: map[string]string{"Content-Type": "application/json"}}
}
func completeNativePassthrough(ctx *schemas.BifrostContext, client *bifrost.Bifrost, req *schemas.BifrostChatRequest, tools, signatureOptional bool) (routing.Completion, error) {
	out, fail := client.Passthrough(ctx, schemas.Anthropic, nativePassthroughRequest(req))
	if fail != nil {
		return routing.Completion{}, anthropicUpstreamError(ctx, fail)
	}
	if out == nil {
		return routing.Completion{}, nativeError(502)
	}
	if out.StatusCode != 200 {
		return routing.Completion{}, nativeReportedError(out.StatusCode, out.Body)
	}
	var native anthropic.AnthropicMessageResponse
	if json.Unmarshal(out.Body, &native) != nil {
		return routing.Completion{}, nativeError(502)
	}
	return anthropicUpstreamCompletion(native.ToBifrostChatResponse(ctx), out.Body, tools, signatureOptional)
}

// Present the raw SDK transport chunks as one reader. Framing, bounds and
// terminal verification remain in the same native reader used for Chat streams.
type nativeTransportReader struct {
	ctx     *schemas.BifrostContext
	ch      chan *schemas.BifrostStreamChunk
	pending []byte
}

func (r *nativeTransportReader) fill() error {
	for len(r.pending) == 0 {
		select {
		case <-r.ctx.Done():
			return anthropicUpstreamError(r.ctx, nil)
		case chunk, ok := <-r.ch:
			if !ok {
				return io.EOF
			}
			if chunk == nil {
				continue
			}
			if chunk.BifrostError != nil {
				return anthropicUpstreamError(r.ctx, chunk.BifrostError)
			}
			out := chunk.BifrostPassthroughResponse
			if out == nil {
				return nativeError(502)
			}
			if out.StatusCode != 200 {
				return nativeReportedError(out.StatusCode, out.Body)
			}
			r.pending = out.Body
		}
	}
	return nil
}
func (r *nativeTransportReader) Read(p []byte) (int, error) {
	if err := r.fill(); err != nil {
		return 0, err
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func streamNativePassthrough(ctx *schemas.BifrostContext, client *bifrost.Bifrost, req *schemas.BifrostChatRequest, tools, signatureOptional bool, release func()) (routing.EventStream, error) {
	raw, fail := client.PassthroughStream(ctx, schemas.Anthropic, nativePassthroughRequest(req))
	if fail != nil {
		err := anthropicUpstreamError(ctx, fail)
		ctx.Cancel()
		release()
		return nil, err
	}
	state := &anthropicUpstreamValidation{tools: tools, signatureOptional: signatureOptional}
	ch := make(chan *schemas.BifrostStreamChunk)
	go func() {
		defer close(ch)
		conversion := anthropic.NewAnthropicStreamState()
		id, model := "", ""
		created := int(time.Now().Unix())
		send := func(chunk *schemas.BifrostStreamChunk) bool {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		failure := func(err error) {
			state.mu.Lock()
			state.failure = err
			state.mu.Unlock()
			send(&schemas.BifrostStreamChunk{BifrostError: &schemas.BifrostError{IsBifrostError: true}})
		}
		transport := &nativeTransportReader{ctx: ctx, ch: raw}
		// Passthrough filters Content-Encoding without decoding SSE. Detect
		// gzip by its magic bytes (which cannot start a valid SSE field).
		buffered := bufio.NewReader(&nativeBoundedReader{reader: transport, remaining: nativeStreamLimit + 1})
		prefix, err := buffered.Peek(2)
		if err != nil {
			failure(nativeReaderError(err))
			return
		}
		var source io.Reader = buffered
		if prefix[0] == 0x1f && prefix[1] == 0x8b {
			zip, err := gzip.NewReader(buffered)
			if err != nil {
				failure(nativeError(502))
				return
			}
			defer zip.Close()
			source = zip
		}
		reader := newAnthropicUpstreamReader(source, state)
		for {
			_, body, err := reader.ReadEvent()
			if err == io.EOF {
				return
			}
			if err != nil {
				failure(err)
				return
			}
			var event anthropic.AnthropicStreamEvent
			if json.Unmarshal(body, &event) != nil {
				failure(nativeError(502))
				return
			}
			if event.Type == anthropic.AnthropicStreamEventTypeMessageStart && event.Message != nil {
				id, model = event.Message.ID, event.Message.Model
			}
			out, fail, _ := event.ToBifrostChatCompletionStream(ctx, "", conversion)
			if fail != nil {
				failure(anthropicUpstreamError(ctx, fail))
				return
			}
			if out != nil {
				out.ID, out.Model, out.Created = id, model, created
				if !send(&schemas.BifrostStreamChunk{BifrostChatResponse: out}) {
					return
				}
			}
		}
	}()
	return &anthropicUpstreamStream{ctx: ctx, ch: ch, state: state, release: release}, nil
}
