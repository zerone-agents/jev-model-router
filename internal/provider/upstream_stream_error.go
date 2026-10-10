package provider

import (
	"encoding/json"
	"io"
	"sync"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

type capturedErrorKey struct{}
type capturedError struct {
	sync.Mutex
	err *routing.UpstreamError
}

// The SDK discards RawResponse for OpenAI SSE errors. Capture only error
// frames at its reader boundary; keep its normal parsing and terminal behavior.
func captureStreamErrors(ctx *schemas.BifrostContext) {
	state := &capturedError{}
	ctx.SetValue(capturedErrorKey{}, state)
	ctx.SetValue(schemas.BifrostContextKeySSEReaderFactory, &providerUtils.SSEReaderFactory{
		NewDataReader: func(reader io.Reader) providerUtils.SSEDataReader {
			return &errorDataReader{SSEDataReader: providerUtils.GetSSEDataReader(nil, reader), state: state, secret: upstreamSecret(ctx)}
		},
	})
}

type errorDataReader struct {
	providerUtils.SSEDataReader
	state  *capturedError
	secret string
}

func (r *errorDataReader) ReadDataLine() ([]byte, error) {
	b, err := r.SSEDataReader.ReadDataLine()
	if err == nil {
		var v struct {
			Error map[string]any `json:"error"`
		}
		if json.Unmarshal(b, &v) == nil && v.Error != nil {
			// Match the SDK's error branch. Malformed events remain parse errors.
			if message, ok := v.Error["message"].(string); ok && message != "" {
				details := routing.SanitizeUpstream(b, r.secret)
				r.state.Lock()
				r.state.err = &routing.UpstreamError{Status: 502, ReportedCode: routing.ReportedErrorCode(v.Error), Details: details, Body: map[string]any{"message": "upstream stream failed", "type": "upstream_error", "code": "upstream_error", "param": nil}}
				r.state.Unlock()
			}
		}
	}
	return b, err
}
func (r *errorDataReader) SawDoneMarker() bool {
	return providerUtils.SSEStreamEndedOnMarker(r.SSEDataReader)
}
func (r *errorDataReader) EndOnCommentAfterFinish() {
	providerUtils.SSEEndOnCommentAfterFinish(r.SSEDataReader)
}
func (r *errorDataReader) EndedOnComment() bool {
	return providerUtils.SSEEndedOnComment(r.SSEDataReader)
}
