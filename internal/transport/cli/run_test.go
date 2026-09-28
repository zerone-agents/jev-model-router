package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHelpOffline(t *testing.T) {
	var out, err bytes.Buffer
	if Run(context.Background(), []string{"--help"}, nil, &out, &err) != 0 || !strings.Contains(out.String(), "schema") {
		t.Fatal(out.String(), err.String())
	}
}
func TestCLIJSONStdinAndExitCode(t *testing.T) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "sentinel")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c struct{ Input map[string]any }
		json.NewDecoder(r.Body).Decode(&c)
		if c.Input["id"] != "m" || r.Header.Get("Authorization") != "Bearer sentinel" {
			t.Error("wrong input")
		}
		w.WriteHeader(409)
		w.Write([]byte(`{"ok":false,"error":{"code":"config_conflict","message":"version conflict"},"warnings":[],"meta":{}}`))
	}))
	defer s.Close()
	var out, err bytes.Buffer
	code := Run(context.Background(), []string{"call", "models.get", "--url", s.URL, "--json", "-"}, strings.NewReader(`{"id":"m"}`), &out, &err)
	if code != 5 || !json.Valid(out.Bytes()) || strings.Contains(err.String(), "sentinel") {
		t.Fatalf("%d %s %s", code, out.String(), err.String())
	}
}
