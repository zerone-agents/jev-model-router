package provider

import "encoding/json"

// WithEffectiveEffort preserves the native request, including opaque signed
// history. The original prepared request remains reusable across attempts.
func (p *PreparedMessages) WithEffectiveEffort(effort string) (*PreparedMessages, error) {
	if effort == "" {
		return p, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(p.body, &fields); err != nil {
		return nil, err
	}
	output := map[string]json.RawMessage{}
	if raw := fields["output_config"]; raw != nil {
		if err := json.Unmarshal(raw, &output); err != nil {
			return nil, err
		}
	}
	output["effort"], _ = json.Marshal(effort)
	fields["output_config"], _ = json.Marshal(output)
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return PrepareMessages(body)
}
