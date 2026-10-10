package provider

import (
	"bytes"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"math/big"
)

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, nativeError(502)
	}
	return obj, nil
}
func rawString(fields map[string]json.RawMessage, key string) (string, bool) {
	var s string
	raw, ok := fields[key]
	if !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}
func fieldsAllowed(fields map[string]json.RawMessage, names ...string) bool {
	allowed := map[string]bool{}
	for _, s := range names {
		allowed[s] = true
	}
	for k := range fields {
		if !allowed[k] {
			return false
		}
	}
	return true
}
func validateMessagesUsage(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	fields, err := rawObject(raw)
	if err != nil {
		return err
	}
	for key, value := range fields {
		switch key {
		case "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens", "reasoning_tokens", "text_tokens", "image_tokens", "audio_tokens", "web_search_requests", "web_fetch_requests":
			var number json.Number
			if json.Unmarshal(value, &number) != nil || len(value) == 0 || value[0] == '"' || string(value) == "null" {
				return nativeError(502)
			}
			n, ok := new(big.Rat).SetString(string(number))
			if !ok || !n.IsInt() || n.Sign() < 0 {
				return nativeError(502)
			}
		case "cache_creation", "output_tokens_details", "input_tokens_details", "server_tool_use":
			if err := validateMessagesUsage(value); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateMessagesBlock(raw json.RawMessage, optional, stream bool) (nativeInputBlock, error) {
	fields, err := rawObject(raw)
	if err != nil {
		return nativeInputBlock{}, err
	}
	var b nativeInputBlock
	if json.Unmarshal(raw, &b) != nil {
		return b, nativeError(502)
	}
	required := func(key string) bool { _, ok := rawString(fields, key); return ok }
	switch b.Type {
	case "text":
		if !fieldsAllowed(fields, "type", "text") || !required("text") {
			return b, nativeError(502)
		}
	case "thinking":
		if !fieldsAllowed(fields, "type", "thinking", "signature") || !required("thinking") {
			return b, nativeError(502)
		}
		if fields["signature"] != nil && !required("signature") {
			return b, nativeError(502)
		}
		if !stream && !optional && b.Signature == "" {
			return b, nativeError(502)
		}
	case "redacted_thinking":
		if !fieldsAllowed(fields, "type", "data") || !required("data") || b.Data == "" {
			return b, nativeError(502)
		}
	case "tool_use":
		if !fieldsAllowed(fields, "type", "id", "name", "input") || b.ID == "" || b.Name == "" || classifyObjectPrefix(b.Input) != objectPrefixComplete {
			return b, nativeError(502)
		}
	default:
		return b, nativeError(502)
	}
	return b, nil
}
func validateMessagesStop(stop string, sequence json.RawMessage, types []string, p *PreparedMessages) error {
	switch stop {
	case "end_turn", "max_tokens", "stop_sequence", "tool_use", "refusal":
	default:
		return nativeError(502)
	}
	if len(types) == 0 && stop != "refusal" {
		return nativeError(502)
	}
	tools := false
	for _, typ := range types {
		tools = tools || typ == "tool_use"
	}
	if stop == "tool_use" && !tools {
		return nativeError(502)
	}
	if stop == "stop_sequence" {
		var value string
		if json.Unmarshal(sequence, &value) != nil || value == "" {
			return nativeError(502)
		}
		var doc messagesDocument
		_ = json.Unmarshal(p.body, &doc)
		for _, s := range doc.StopSequences {
			if s == value {
				return nil
			}
		}
		return nativeError(502)
	}
	if len(sequence) > 0 && string(sequence) != "null" {
		return nativeError(502)
	}
	return nil
}
func validateNativeMessagesResponse(body json.RawMessage, target routing.Target, p *PreparedMessages) (json.RawMessage, error) {
	if len(body) > nativeStreamLimit || uniqueJSON(body) != nil {
		return nil, nativeError(502)
	}
	fields, err := rawObject(body)
	if err != nil {
		return nil, err
	}
	typ, _ := rawString(fields, "type")
	role, _ := rawString(fields, "role")
	id, _ := rawString(fields, "id")
	_, modelOK := rawString(fields, "model")
	if typ != "message" || role != "assistant" || id == "" || !modelOK {
		return nil, nativeError(502)
	}
	var content []json.RawMessage
	if json.Unmarshal(fields["content"], &content) != nil || content == nil {
		return nil, nativeError(502)
	}
	ids := map[string]bool{}
	types := []string{}
	for _, raw := range content {
		b, err := validateMessagesBlock(raw, nativeSignatureOptional(target), false)
		if err != nil {
			return nil, err
		}
		if b.Type == "tool_use" {
			if ids[b.ID] {
				return nil, nativeError(502)
			}
			ids[b.ID] = true
		}
		types = append(types, b.Type)
	}
	stop, _ := rawString(fields, "stop_reason")
	if err = validateMessagesStop(stop, fields["stop_sequence"], types, p); err != nil {
		return nil, err
	}
	if err = validateMessagesUsage(fields["usage"]); err != nil {
		return nil, err
	}
	fields["model"], _ = json.Marshal(target.Model.ID)
	return json.Marshal(fields)
}
