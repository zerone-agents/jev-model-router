package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardURL(t *testing.T) {
	for _, tc := range []struct{ explicit, env, listen, want string }{{"https://example.com/", "http://ignored", "", "https://example.com/dashboard/"}, {"", "http://localhost:9", "", "http://localhost:9/dashboard/"}, {"", "", "0.0.0.0:8080", "http://127.0.0.1:8080/dashboard/"}, {"", "", "[::]:8080", "http://[::1]:8080/dashboard/"}} {
		got, e := dashboardURL(tc.explicit, tc.env, tc.listen)
		if e != nil || got != tc.want {
			t.Fatalf("%q %v", got, e)
		}
	}
	for _, bad := range []string{"file:///tmp/x", "javascript:evil()", "https://secret@example.com", "https://x/?token=secret", "https://x/#secret", "https://x/prefix", "http://x:bad", "https://x\\evil"} {
		if _, e := dashboardURL(bad, "", ""); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatalf("bad URL accepted or exposed: %s", bad)
		}
	}
}
func TestDashboardNoOpenNeedsNoTokenOrNetwork(t *testing.T) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "")
	var out, err bytes.Buffer
	code := Run(context.Background(), []string{"dashboard", "--url", "http://localhost:1", "--no-open"}, nil, &out, &err)
	if code != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"opened":false`) {
		t.Fatalf("%d %s", code, out.String())
	}
}
func TestDashboardProbeAndOpener(t *testing.T) {
	opened := false
	var out bytes.Buffer
	probe := func(context.Context, string) error { return errors.New("unavailable") }
	open := func(context.Context, string) error { opened = true; return nil }
	if runDashboard(context.Background(), "http://localhost/dashboard/", false, &out, probe, open) == 0 || opened {
		t.Fatal("opened unavailable instance")
	}
	out.Reset()
	probe = func(context.Context, string) error { return nil }
	if runDashboard(context.Background(), "http://localhost/dashboard/", false, &out, probe, open) != 0 || !opened {
		t.Fatal("did not open")
	}
	out.Reset()
	if runDashboard(context.Background(), "http://localhost/dashboard/", false, &out, probe, func(context.Context, string) error { return errors.New("fail") }) == 0 || !strings.Contains(out.String(), "http://localhost/dashboard/") {
		t.Fatal("missing recovery URL")
	}
}
func TestDashboardProbeRejectsRedirects(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credential sent")
		}
		http.Redirect(w, r, "/elsewhere", 302)
	}))
	defer s.Close()
	if probeDashboard(context.Background(), s.URL) == nil {
		t.Fatal("followed redirect")
	}
}
