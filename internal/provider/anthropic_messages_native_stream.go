package provider

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	bifrost "github.com/maximhq/bifrost/core"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"strings"
	"sync"
)

// Unlike LimitReader, this distinguishes clean EOF at the limit from overflow.
type messagesBoundedReader struct {
	reader    io.Reader
	remaining int
}

func (r *messagesBoundedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		var one [1]byte
		n, err := r.reader.Read(one[:])
		if n > 0 {
			return 0, nativeError(502)
		}
		return 0, err
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= n
	return n, err
}

type nativeMessagesValidation struct {
	target           routing.Target
	prepared         *PreparedMessages
	started, stopped bool
	open             int
	types            []string
	ids              map[string]bool
	block            nativeInputBlock
	arguments        strings.Builder
	hasDelta         bool
	prefixes         map[int]objectPrefixKind
}

func newNativeMessagesEventReader(reader io.Reader, target routing.Target, p *PreparedMessages) providerUtils.SSEEventReader {
	state := &nativeMessagesValidation{target: target, prepared: p, open: -1, ids: map[string]bool{}, prefixes: map[int]objectPrefixKind{}}
	scanner := bufio.NewScanner(&messagesBoundedReader{reader: reader, remaining: nativeStreamLimit})
	scanner.Buffer(make([]byte, 8192), nativeFrameLimit+1)
	return &anthropicUpstreamReader{scanner: scanner, accept: state.accept}
}
func (s *nativeMessagesValidation) accept(typ string, raw []byte) error {
	if uniqueJSON(raw) != nil {
		return nativeError(502)
	}
	fields, err := rawObject(raw)
	if err != nil {
		return err
	}
	eventType, _ := rawString(fields, "type")
	if eventType != typ {
		return nativeError(502)
	}
	if typ == "error" {
		var e struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(fields["error"], &e) != nil {
			return nativeError(502)
		}
		return nativeEventError(e.Type)
	}
	if typ == "ping" {
		return nil
	}
	if typ == "message_start" {
		if s.started {
			return nativeError(502)
		}
		msg, err := rawObject(fields["message"])
		if err != nil {
			return err
		}
		id, _ := rawString(msg, "id")
		role, _ := rawString(msg, "role")
		mt, _ := rawString(msg, "type")
		_, model := rawString(msg, "model")
		var blocks []json.RawMessage
		if id == "" || role != "assistant" || mt != "message" || !model || json.Unmarshal(msg["content"], &blocks) != nil || blocks == nil || len(blocks) > 0 {
			return nativeError(502)
		}
		for _, key := range []string{"stop_reason", "stop_sequence"} {
			if v := msg[key]; len(v) > 0 && string(v) != "null" {
				return nativeError(502)
			}
		}
		if err = validateMessagesUsage(msg["usage"]); err != nil {
			return err
		}
		s.started = true
		return nil
	}
	if !s.started {
		return nativeError(502)
	}
	getIndex := func() (int, bool) {
		var n int
		raw, ok := fields["index"]
		if !ok || string(raw) == "null" || json.Unmarshal(raw, &n) != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	switch typ {
	case "content_block_start":
		n, ok := getIndex()
		if !ok || s.stopped || s.open != -1 || n != len(s.types) {
			return nativeError(502)
		}
		b, err := validateMessagesBlock(fields["content_block"], nativeSignatureOptional(s.target), true)
		if err != nil {
			return err
		}
		if b.Type == "tool_use" {
			if s.ids[b.ID] {
				return nativeError(502)
			}
			s.ids[b.ID] = true
		}
		s.types = append(s.types, b.Type)
		s.open = n
		s.block = b
		s.arguments.Reset()
		s.hasDelta = false
	case "content_block_delta":
		n, ok := getIndex()
		if !ok || s.stopped || s.open < 0 || n != s.open {
			return nativeError(502)
		}
		d, err := rawObject(fields["delta"])
		if err != nil {
			return err
		}
		dt, _ := rawString(d, "type")
		switch dt {
		case "text_delta":
			if _, ok := rawString(d, "text"); !ok || s.block.Type != "text" || !fieldsAllowed(d, "type", "text") {
				return nativeError(502)
			}
		case "thinking_delta":
			if _, ok := rawString(d, "thinking"); !ok || s.block.Type != "thinking" || !fieldsAllowed(d, "type", "thinking") {
				return nativeError(502)
			}
		case "signature_delta":
			value, ok := rawString(d, "signature")
			if !ok || s.block.Type != "thinking" || !fieldsAllowed(d, "type", "signature") {
				return nativeError(502)
			}
			if value != "" {
				s.block.Signature = "present"
			}
		case "input_json_delta":
			value, ok := rawString(d, "partial_json")
			if !ok || s.block.Type != "tool_use" || !fieldsAllowed(d, "type", "partial_json") {
				return nativeError(502)
			}
			if !s.hasDelta {
				var obj map[string]json.RawMessage
				_ = json.Unmarshal(s.block.Input, &obj)
				if len(obj) > 0 {
					return nativeError(502)
				}
			}
			s.hasDelta = true
			s.arguments.WriteString(value)
		default:
			return nativeError(502)
		}
	case "content_block_stop":
		n, ok := getIndex()
		if !ok || s.open < 0 || n != s.open {
			return nativeError(502)
		}
		if s.block.Type == "thinking" && !nativeSignatureOptional(s.target) && s.block.Signature == "" {
			return nativeError(502)
		}
		if s.block.Type == "tool_use" {
			raw := s.block.Input
			if s.hasDelta {
				raw = []byte(s.arguments.String())
			}
			kind := classifyObjectPrefix(raw)
			if kind == objectPrefixInvalid {
				return nativeError(502)
			}
			s.prefixes[s.open] = kind
		}
		s.open = -1
	case "message_delta":
		if s.open != -1 || s.stopped {
			return nativeError(502)
		}
		d, err := rawObject(fields["delta"])
		if err != nil {
			return err
		}
		stop, ok := rawString(d, "stop_reason")
		if !ok || !fieldsAllowed(d, "stop_reason", "stop_sequence") {
			return nativeError(502)
		}
		if err = validateMessagesStop(stop, d["stop_sequence"], s.types, s.prepared); err != nil {
			return err
		}
		for index, kind := range s.prefixes {
			if kind != objectPrefixComplete && (kind != objectPrefixIncomplete || stop != "max_tokens" || index != len(s.types)-1) {
				return nativeError(502)
			}
		}
		if err = validateMessagesUsage(fields["usage"]); err != nil {
			return err
		}
		s.stopped = true
	case "message_stop":
		if !s.stopped || s.open != -1 {
			return nativeError(502)
		}
	default:
		return nativeError(502)
	}
	return nil
}

type nativeMessagesStream struct {
	ctx       *schemas.BifrostContext
	transport io.Reader
	reader    providerUtils.SSEEventReader
	zip       *gzip.Reader
	target    routing.Target
	prepared  *PreparedMessages
	release   func()
	once      sync.Once
	ended     bool
}

func streamNativeMessages(ctx *schemas.BifrostContext, client *bifrost.Bifrost, target routing.Target, p *PreparedMessages, release func()) (MessageStream, error) {
	body, err := p.nativeBody(target)
	if err != nil {
		ctx.Cancel()
		release()
		return nil, err
	}
	ch, failure := client.PassthroughStream(ctx, schemas.Anthropic, nativeMessagesTransportRequest(target.Model.UpstreamName, body))
	if ctx.Err() != nil || failure != nil {
		err = anthropicUpstreamError(ctx, failure)
		ctx.Cancel()
		release()
		return nil, err
	}
	return &nativeMessagesStream{ctx: ctx, transport: &nativeTransportReader{ctx: ctx, ch: ch}, target: target, prepared: p, release: release}, nil
}
func (s *nativeMessagesStream) Close() error {
	s.once.Do(func() { s.ctx.Cancel(); s.release() })
	return nil
}
func (s *nativeMessagesStream) Next(ctx context.Context) (event MessageEvent, err error) {
	if s.ended {
		return event, io.EOF
	}
	stop := context.AfterFunc(ctx, s.ctx.Cancel)
	defer stop()
	defer func() {
		if ctx.Err() != nil {
			err = providerContextError(ctx)
		} else if s.ctx.Err() != nil {
			err = anthropicUpstreamError(s.ctx, nil)
		}
		if err != nil {
			event = MessageEvent{}
			s.ended = true
			_ = s.Close()
		} else if event.Terminal {
			s.ended = true
			_ = s.Close()
		}
	}()
	if ctx.Err() != nil {
		return event, providerContextError(ctx)
	}
	if s.ctx.Err() != nil {
		return event, anthropicUpstreamError(s.ctx, nil)
	}
	if s.reader == nil {
		buffered := bufio.NewReader(&messagesBoundedReader{reader: s.transport, remaining: nativeStreamLimit})
		prefix, e := buffered.Peek(2)
		if e != nil {
			return event, nativeReaderError(e)
		}
		var source io.Reader = buffered
		if prefix[0] == 0x1f && prefix[1] == 0x8b {
			s.zip, e = gzip.NewReader(buffered)
			if e != nil {
				return event, nativeError(502)
			}
			source = s.zip
		}
		s.reader = newNativeMessagesEventReader(source, s.target, s.prepared)
	}
	typ, body, err := s.reader.ReadEvent()
	if err != nil {
		return event, err
	}
	if typ == "message_start" {
		var fields, msg map[string]json.RawMessage
		_ = json.Unmarshal(body, &fields)
		_ = json.Unmarshal(fields["message"], &msg)
		msg["model"], _ = json.Marshal(s.target.Model.ID)
		fields["message"], _ = json.Marshal(msg)
		body, _ = json.Marshal(fields)
	}
	return MessageEvent{Type: typ, Data: body, Terminal: typ == "message_stop"}, nil
}
func providerContextError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return nativeError(504)
	}
	return nativeError(499)
}
