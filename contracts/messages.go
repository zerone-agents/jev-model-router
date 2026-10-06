package contracts

import "encoding/json"

// MessagesInvalidField only returns schema-owned names, never attacker-chosen
// field names or the validation library's message containing request values.
func MessagesInvalidField(input json.RawMessage) string {
	once.Do(initSchemas)
	var obj map[string]json.RawMessage
	if json.Unmarshal(input, &obj) != nil || obj == nil {
		return "body"
	}
	for _, key := range []string{"model", "messages", "max_tokens"} {
		if _, ok := obj[key]; !ok {
			return key
		}
	}
	for _, key := range []string{"model", "messages", "max_tokens", "system", "stream", "temperature", "top_p", "stop_sequences", "tools", "tool_choice", "thinking", "output_config"} {
		if raw, ok := obj[key]; ok {
			value, err := Decode(raw)
			if err != nil || validators["messages"].Properties[key].Validate(value) != nil {
				return key
			}
		}
	}
	return "unknown field"
}
