package contracts

import "encoding/json"

// SessionContract is also embedded verbatim in runtime discovery.
type SessionContract struct {
	TTLSeconds     int             `json:"ttl_seconds"`
	MaxSessions    int             `json:"max_sessions"`
	TokenBytes     int             `json:"token_bytes"`
	BodyLimitBytes int64           `json:"body_limit_bytes"`
	InputSchema    json.RawMessage `json:"input_schema"`
	Cookie         struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"cookie"`
}

func SessionPolicy() json.RawMessage {
	b, err := files.ReadFile("schemas/session.json")
	if err != nil {
		panic(err)
	}
	return b
}
func SessionLimits() SessionContract {
	var p SessionContract
	if err := json.Unmarshal(SessionPolicy(), &p); err != nil {
		panic(err)
	}
	return p
}
