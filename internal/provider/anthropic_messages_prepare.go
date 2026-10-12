package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"math/big"
	"unicode/utf8"
)

type messagesDocument struct {
	Model     string               `json:"model"`
	MaxTokens json.Number          `json:"max_tokens"`
	Stream    bool                 `json:"stream"`
	System    json.RawMessage      `json:"system"`
	Messages  []nativeInputMessage `json:"messages"`
	Tools     []json.RawMessage    `json:"tools"`
	Thinking  *struct {
		Type   string      `json:"type"`
		Budget json.Number `json:"budget_tokens"`
	} `json:"thinking"`
	StopSequences []string `json:"stop_sequences"`
}
type nativeInputMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type nativeInputBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	Data      string          `json:"data"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	Source    json.RawMessage `json:"source"`
	Cache     json.RawMessage `json:"cache_control"`
	IsError   bool            `json:"is_error"`
}

func inputBlocks(raw json.RawMessage) []nativeInputBlock {
	var b []nativeInputBlock
	_ = json.Unmarshal(raw, &b)
	return b
}

// Token traversal rejects ambiguous duplicate names at every depth without
// converting JSON numbers. The decoder enforces the JSON nesting limit.
func uniqueJSON(raw []byte) error {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return errors.New("invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	type frame struct {
		object, key bool
		keys        map[string]bool
	}
	var stack []frame
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid JSON")
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].key = true
				}
				stack = append(stack, frame{object: delim == '{', key: delim == '{', keys: map[string]bool{}})
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(stack) > 0 {
			f := &stack[len(stack)-1]
			if f.object {
				if f.key {
					key, ok := tok.(string)
					if !ok || f.keys[key] {
						return errors.New("duplicate JSON key")
					}
					f.keys[key] = true
				}
				f.key = !f.key
			}
		}
	}
}
func PrepareMessages(body json.RawMessage) (*PreparedMessages, error) {
	if uniqueJSON(body) != nil {
		return nil, routing.Fail("invalid_request", "Messages body must be unambiguous valid UTF-8 JSON")
	}
	if contracts.Validate("messages", body) != nil {
		return nil, routing.Fail("invalid_request", "invalid or unsupported Messages field: "+contracts.MessagesInvalidField(body))
	}
	var doc messagesDocument
	if json.Unmarshal(body, &doc) != nil {
		return nil, routing.Fail("invalid_request", "invalid Messages body")
	}
	if doc.Thinking != nil && doc.Thinking.Type == "enabled" {
		budget, ok := new(big.Rat).SetString(string(doc.Thinking.Budget))
		limit, ok2 := new(big.Rat).SetString(string(doc.MaxTokens))
		if !ok || !ok2 || budget.Cmp(limit) >= 0 {
			return nil, routing.Fail("invalid_request", "thinking budget must be less than max_tokens")
		}
	}
	cacheCount := 0
	shortCache := false
	cache := func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return nil
		}
		cacheCount++
		var c struct {
			TTL string `json:"ttl"`
		}
		_ = json.Unmarshal(raw, &c)
		if cacheCount > 4 || c.TTL == "1h" && shortCache {
			return routing.Fail("invalid_request", "invalid cache_control breakpoint order or count")
		}
		if c.TTL != "1h" {
			shortCache = true
		}
		return nil
	}
	for _, tool := range doc.Tools {
		var b nativeInputBlock
		_ = json.Unmarshal(tool, &b)
		if err := cache(b.Cache); err != nil {
			return nil, err
		}
	}
	for _, b := range inputBlocks(doc.System) {
		if err := cache(b.Cache); err != nil {
			return nil, err
		}
	}
	ids := map[string]bool{}
	pending := map[string]bool{}
	for _, m := range doc.Messages {
		for _, b := range inputBlocks(m.Content) {
			if err := cache(b.Cache); err != nil {
				return nil, err
			}
			switch b.Type {
			case "thinking", "redacted_thinking":
				if m.Role != "assistant" {
					return nil, routing.Fail("invalid_request", "thinking history requires assistant role")
				}
			case "tool_use":
				if m.Role != "assistant" || ids[b.ID] {
					return nil, routing.Fail("invalid_request", "invalid or duplicate tool use")
				}
				ids[b.ID] = true
				pending[b.ID] = true
			case "tool_result":
				if m.Role != "user" || !pending[b.ToolUseID] {
					return nil, routing.Fail("invalid_request", "unmatched or duplicate tool result")
				}
				delete(pending, b.ToolUseID)
			}
		}
	}
	var source struct {
		Messages []routing.Message `json:"messages"`
	}
	_ = json.Unmarshal(body, &source)
	return &PreparedMessages{body: append(json.RawMessage(nil), body...), stops: len(doc.StopSequences) > 0, summary: routing.RequestSummary(routing.Request{Messages: source.Messages})}, nil
}
func (p *PreparedMessages) nativeBody(t routing.Target) (json.RawMessage, error) {
	if !nativeSignatureOptional(t) {
		var doc messagesDocument
		_ = json.Unmarshal(p.body, &doc)
		for _, m := range doc.Messages {
			for _, b := range inputBlocks(m.Content) {
				if b.Type == "thinking" && b.Signature == "" {
					return nil, routing.Fail("unsupported_request", "native thinking history requires a signature for this target")
				}
			}
		}
	}

	var fields map[string]json.RawMessage
	if json.Unmarshal(p.body, &fields) != nil {
		return nil, routing.Fail("internal_error", "cannot build native Messages request")
	}
	fields["model"], _ = json.Marshal(t.Model.UpstreamName)
	return json.Marshal(fields)
}
func rejectNativeChatFields(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if fields["top_k"] != nil || fields["metadata"] != nil {
		return routing.Fail("unsupported_request", "native sampling or metadata cannot be preserved by Chat adapter")
	}
	var doc messagesDocument
	_ = json.Unmarshal(raw, &doc)
	check := func(b nativeInputBlock) error {
		if len(b.Cache) > 0 || b.Type == "redacted_thinking" {
			return routing.Fail("unsupported_request", "native cache or redacted history cannot be preserved by Chat adapter")
		}
		return nil
	}
	for _, tool := range doc.Tools {
		var b nativeInputBlock
		_ = json.Unmarshal(tool, &b)
		if err := check(b); err != nil {
			return err
		}
	}
	for _, b := range inputBlocks(doc.System) {
		if err := check(b); err != nil {
			return err
		}
	}
	for _, m := range doc.Messages {
		for _, b := range inputBlocks(m.Content) {
			if err := check(b); err != nil {
				return err
			}
		}
	}
	return nil
}
