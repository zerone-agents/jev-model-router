package contracts

import "encoding/json"

// Enrich output schemas from one shared health schema; write inputs remain unchanged.
func addHealthSchemas() {
	b, e := files.ReadFile("schemas/health.json")
	if e != nil {
		panic(e)
	}
	var h any
	if e = json.Unmarshal(b, &h); e != nil {
		panic(e)
	}
	exclusion := map[string]any{"type": "array", "maxItems": 200, "items": map[string]any{"type": "object", "properties": map[string]any{"model_id": map[string]any{"type": "string"}, "state": map[string]any{"const": "isolated"}, "reason": map[string]any{"type": "string"}, "next_probe_at": map[string]any{"type": []string{"string", "null"}}}, "required": []string{"model_id", "state", "reason", "next_probe_at"}, "additionalProperties": false}}
	for i := range caps {
		var out map[string]any
		json.Unmarshal(caps[i].OutputSchema, &out)
		p := out["properties"].(map[string]any)
		switch caps[i].ID {
		case "models.list":
			p["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["health"] = h
		case "models.get":
			p["resource"].(map[string]any)["properties"].(map[string]any)["health"] = h
		case "models.test":
			p["health"] = h
			p["persisted"] = map[string]any{"type": "boolean"}
		case "route.inspect":
			p["health_exclusions"] = exclusion
			p["health_exclusions_truncated"] = map[string]any{"type": "boolean"}
		}
		caps[i].OutputSchema, _ = json.Marshal(out)
	}
}
