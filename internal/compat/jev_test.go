package compat

import (
	"encoding/json"
	"os"
	"testing"
)

func TestJevChoiceContract(t *testing.T) {
	b, e := os.ReadFile("testdata/jev-choice.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Answers map[string]struct {
			Type   string `json:"type"`
			Choice string `json:"choice"`
		} `json:"answers"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if v.Answers["model"].Choice != "m0" || v.Answers["model"].Type != "choice" {
		t.Fatal("native choice fixture malformed")
	}
}
