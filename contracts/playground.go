package contracts

import "encoding/json"

// PlaygroundPolicy is the shared discovery and configuration source.
func PlaygroundPolicy() json.RawMessage {
	b, err := files.ReadFile("schemas/playground.json")
	if err != nil {
		panic(err)
	}
	return b
}
