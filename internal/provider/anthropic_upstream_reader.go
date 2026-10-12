package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"strings"
	"sync"
)

const nativeStreamLimit = 16 * 1024 * 1024
const nativeFrameLimit = 1024 * 1024

// The reader is the sole body consumer. State is shared only with this request's
// output wrapper; snapshots are taken after the SDK channel has closed.
type anthropicUpstreamValidation struct {
	secret            string
	mu                sync.Mutex
	terminalVerified  bool
	failure           error
	body              nativeResponse
	started           bool
	open              int
	stopped           bool
	tools             bool
	signatureOptional bool
	ids               map[string]bool
	argument          strings.Builder
}
type nativeBoundedReader struct {
	reader    io.Reader
	remaining int
}

func (r *nativeBoundedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, nativeError(502)
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, e := r.reader.Read(p)
	r.remaining -= n
	return n, e
}

type anthropicUpstreamReader struct {
	scanner  *bufio.Scanner
	state    *anthropicUpstreamValidation
	finished bool
}

func newAnthropicUpstreamReader(reader io.Reader, state *anthropicUpstreamValidation) providerUtils.SSEEventReader {
	scanner := bufio.NewScanner(&nativeBoundedReader{reader: reader, remaining: nativeStreamLimit + 1})
	scanner.Buffer(make([]byte, 8192), nativeFrameLimit+1)
	return &anthropicUpstreamReader{scanner: scanner, state: state}
}
func (r *anthropicUpstreamReader) fail(e error) (string, []byte, error) {
	r.state.mu.Lock()
	r.state.failure = e
	r.state.mu.Unlock()
	return "", nil, e
}
func (r *anthropicUpstreamReader) ReadEvent() (string, []byte, error) {
	if r.finished {
		return "", nil, io.EOF
	}
	typ := ""
	var data strings.Builder
	present := false
	size := 0
	for r.scanner.Scan() {
		line := r.scanner.Text()
		size += len(line) + 1
		if size > nativeFrameLimit {
			return r.fail(nativeError(502))
		}
		if line == "" {
			if !present {
				size = 0
				continue
			}
			raw := []byte(data.String())
			if typ == "" || len(raw) == 0 || !json.Valid(raw) {
				return r.fail(nativeError(502))
			}
			r.state.mu.Lock()
			e := r.state.accept(typ, raw)
			r.state.mu.Unlock()
			if e != nil {
				return r.fail(e)
			}
			if typ == "message_stop" {
				// Do not let the SDK stop reading until the entire response body is clean.
				for r.scanner.Scan() {
					tail := r.scanner.Text()
					if tail != "" && !strings.HasPrefix(tail, ":") {
						return r.fail(nativeError(502))
					}
				}
				if r.scanner.Err() != nil {
					return r.fail(nativeReaderError(r.scanner.Err()))
				}
				r.state.mu.Lock()
				r.state.terminalVerified = true
				r.state.mu.Unlock()
				r.finished = true
			}
			return typ, raw, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			return r.fail(nativeError(502))
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			if typ != "" {
				return r.fail(nativeError(502))
			}
			typ = value
			present = true
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			present = true
		case "id", "retry":
			present = true
		default:
			return r.fail(nativeError(502))
		}
	}
	if r.scanner.Err() != nil || present {
		return r.fail(nativeReaderError(r.scanner.Err()))
	}
	return r.fail(nativeError(502)) // Plain EOF before verified message_stop is truncation.
}
func (s *anthropicUpstreamValidation) accept(typ string, raw []byte) error {
	var event struct {
		Type    string          `json:"type"`
		Message *nativeResponse `json:"message"`
		Index   *int            `json:"index"`
		Block   *nativeBlock    `json:"content_block"`
		Delta   struct {
			Type      string  `json:"type"`
			Text      *string `json:"text"`
			Thinking  *string `json:"thinking"`
			Signature *string `json:"signature"`
			JSON      *string `json:"partial_json"`
			Stop      *string `json:"stop_reason"`
		} `json:"delta"`
		Usage *nativeUsage `json:"usage"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Type != typ {
		return nativeError(502)
	}
	if typ == "error" {
		err := nativeEventError(event.Error.Type).(*routing.UpstreamError)
		err.Details = routing.SanitizeUpstream(raw, s.secret)
		return err
	}
	if typ == "ping" {
		return nil
	}
	if typ == "message_start" {
		if s.started || event.Message == nil || event.Message.ID == "" || event.Message.Role != "assistant" || event.Message.Type != "message" || len(event.Message.Content) > 0 || event.Message.Stop != "" {
			return nativeError(502)
		}
		s.body = *event.Message
		s.started = true
		s.open = -1
		s.ids = map[string]bool{}
		return nil
	}
	if !s.started {
		return nativeError(502)
	}
	switch typ {
	case "content_block_start":
		if s.stopped || s.open != -1 || event.Index == nil || *event.Index != len(s.body.Content) || event.Block == nil {
			return nativeError(502)
		}
		b := *event.Block
		switch b.Type {
		case "text":
			if b.Text == nil {
				return nativeError(502)
			}
		case "thinking":
			if b.Thinking == nil || (!s.signatureOptional && s.tools && b.Signature != nil && *b.Signature != "") {
				return nativeError(502)
			}
		case "tool_use":
			if b.ID == "" || b.Name == "" || s.ids[b.ID] {
				return nativeError(502)
			}
			s.ids[b.ID] = true
			for _, v := range s.body.Content {
				if !s.signatureOptional && v.Type == "thinking" && v.Signature != nil && *v.Signature != "" {
					return nativeError(502)
				}
			}
			var obj map[string]any
			if json.Unmarshal(b.Input, &obj) != nil || obj == nil {
				return nativeError(502)
			}
			s.argument.Reset()
			if len(obj) > 0 {
				s.argument.Write(b.Input)
			}
		default:
			return nativeError(502)
		}
		if !s.signatureOptional && b.Type == "thinking" && len(s.ids) > 0 && b.Signature != nil && *b.Signature != "" {
			return nativeError(502)
		}
		s.body.Content = append(s.body.Content, b)
		s.open = *event.Index
	case "content_block_delta":
		if s.stopped || event.Index == nil || s.open < 0 || *event.Index != s.open {
			return nativeError(502)
		}
		b := &s.body.Content[s.open]
		switch event.Delta.Type {
		case "text_delta":
			if b.Type != "text" || event.Delta.Text == nil {
				return nativeError(502)
			}
			*b.Text += *event.Delta.Text
		case "thinking_delta":
			if b.Type != "thinking" || event.Delta.Thinking == nil {
				return nativeError(502)
			}
			*b.Thinking += *event.Delta.Thinking
		case "signature_delta":
			if b.Type != "thinking" || event.Delta.Signature == nil || (!s.signatureOptional && *event.Delta.Signature != "" && (s.tools || len(s.ids) > 0)) {
				return nativeError(502)
			}
			if b.Signature == nil {
				v := ""
				b.Signature = &v
			}
			*b.Signature += *event.Delta.Signature
		case "input_json_delta":
			if b.Type != "tool_use" || event.Delta.JSON == nil {
				return nativeError(502)
			}
			s.argument.WriteString(*event.Delta.JSON)
		default:
			return nativeError(502)
		}
	case "content_block_stop":
		if event.Index == nil || s.open < 0 || *event.Index != s.open {
			return nativeError(502)
		}
		b := &s.body.Content[s.open]
		if b.Type == "tool_use" {
			if s.argument.Len() > 0 {
				b.Input = []byte(s.argument.String())
			}
			var obj map[string]any
			if json.Unmarshal(b.Input, &obj) != nil || obj == nil {
				return nativeError(502)
			}
		}
		s.open = -1
	case "message_delta":
		if s.open != -1 {
			return nativeError(502)
		}
		if event.Delta.Stop != nil {
			if s.stopped {
				return nativeError(502)
			}
			if _, e := nativeFinish(*event.Delta.Stop); e != nil {
				return e
			}
			s.body.Stop = *event.Delta.Stop
			s.stopped = true
		}
		if event.Usage != nil {
			if s.body.Usage == nil {
				s.body.Usage = &nativeUsage{}
			}
			a, b := s.body.Usage, event.Usage
			if b.Input != nil {
				a.Input = b.Input
			}
			if b.Output != nil {
				a.Output = b.Output
			}
			if b.Read != nil {
				a.Read = b.Read
			}
			if b.Write != nil {
				a.Write = b.Write
			}
		}
	case "message_stop":
		if !s.stopped || s.open != -1 || len(s.body.Content) == 0 {
			return nativeError(502)
		}
	default:
		return nativeError(502)
	}
	return nil
}

func nativeReaderError(e error) error {
	var upstream *routing.UpstreamError
	if errors.As(e, &upstream) {
		return upstream
	}
	if errors.Is(e, providerUtils.ErrStreamIdleTimeout) {
		return nativeError(504)
	}
	return nativeError(502)
}
