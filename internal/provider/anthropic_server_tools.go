package provider

import "encoding/json"

// Server-executed tools are not client tool calls. Validate their linkage before
// removing them from the SDK conversion input; preserve all other raw fields.
func projectServerTools(raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, nativeError(502)
	}
	var blocks []json.RawMessage
	if json.Unmarshal(fields["content"], &blocks) != nil {
		return nil, nativeError(502)
	}
	calls := map[string]bool{}
	ids := map[string]bool{}
	kept := make([]json.RawMessage, 0, len(blocks))
	for _, rawBlock := range blocks {
		var b nativeBlock
		if json.Unmarshal(rawBlock, &b) != nil {
			return nil, nativeError(502)
		}
		switch b.Type {
		case "server_tool_use", "tool_use":
			if b.ID == "" || ids[b.ID] {
				return nil, nativeError(502)
			}
			ids[b.ID] = true
			if b.Type == "server_tool_use" {
				var input map[string]any
				if b.Name == "" || json.Unmarshal(b.Input, &input) != nil || input == nil {
					return nil, nativeError(502)
				}
				calls[b.ID] = false
				continue
			}
		case "tool_result":
			done, exists := calls[b.ToolUseID]
			if !exists || done || len(b.Content) == 0 || string(b.Content) == "null" {
				return nil, nativeError(502)
			}
			calls[b.ToolUseID] = true
			continue
		}
		kept = append(kept, rawBlock)
	}
	for _, done := range calls {
		if !done {
			return nil, nativeError(502)
		}
	}
	fields["content"], _ = json.Marshal(kept)
	return json.Marshal(fields)
}
