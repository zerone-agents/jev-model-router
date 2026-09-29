package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectNeverGenerates(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/admin/v1/call/route.inspect" || r.Header.Get("Authorization") != "Bearer settings-test" {
			t.Errorf("unexpected endpoint or credential")
			w.WriteHeader(500)
			return
		}
		var body struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input["model"] != "auto" {
			t.Error("invalid inspect input")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"model_id":"fast","candidate_ids":["fast","deep"],"path":"jev_choice","config_version":7}}`))
	}))
	defer server.Close()
	t.Setenv("JEV_ROUTER_URL", server.URL)
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "")
	dir := t.TempDir()
	cases := filepath.Join(dir, "cases.jsonl")
	if err := os.WriteFile(cases, []byte(`{"case_id":"simple","class":"simple","request":{"messages":[{"role":"user","content":"hello"}]},"acceptable_model_ids":["fast"],"quality_criteria":"selection only"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "metrics.jsonl")
	args := []string{"--cases", cases, "--mode", "inspect", "--repeat", "2", "--output", out}
	if err := runArgs(args); err == nil || calls != 0 {
		t.Fatal("paid opt-in must precede network calls")
	}
	args = append(args, "--allow-paid")
	if err := runArgs(args); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("got %d calls", calls)
	}
	file, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	count := 0
	for scan.Scan() {
		count++
		var m metric
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Repeat != count || !m.Acceptable || m.Outcome != "success" || m.Quality != "unknown" || m.GenerationUsage != nil || m.ConfigVersion != 7 || m.Path != "jev_choice" {
			t.Fatalf("unexpected metric: %+v", m)
		}
	}
	if scan.Err() != nil || count != 2 {
		t.Fatal("missing results")
	}
	if err := runArgs(args); err == nil || calls != 2 {
		t.Fatal("must not overwrite output or call upstream again")
	}
}

func TestInspectRetainsFailedAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"data":{"model_id":"unknown","candidate_ids":["fast"],"path":"jev_choice"}}`))
	}))
	defer server.Close()
	t.Setenv("JEV_ROUTER_URL", server.URL)
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "")
	dir := t.TempDir()
	cases := filepath.Join(dir, "cases.jsonl")
	out := filepath.Join(dir, "out.jsonl")
	if err := os.WriteFile(cases, []byte(`{"case_id":"x","request":{},"acceptable_model_ids":["fast"],"quality_criteria":"selection"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runArgs([]string{"--mode", "inspect", "--cases", cases, "--output", out, "--allow-paid"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var m metric
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Outcome != "error" || m.Acceptable {
		t.Fatalf("invalid selection accepted: %+v", m)
	}
}
