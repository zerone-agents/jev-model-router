package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func dashboardURL(explicit, envURL, listen string) (string, error) {
	raw := explicit
	if raw == "" {
		raw = envURL
	}
	if raw == "" {
		host, port, e := net.SplitHostPort(listen)
		if e != nil {
			return "", errors.New("invalid dashboard address")
		}
		switch host {
		case "", "0.0.0.0":
			host = "127.0.0.1"
		case "::":
			host = "::1"
		}
		raw = "http://" + net.JoinHostPort(host, port)
	}
	u, e := url.Parse(raw)
	if e != nil || u == nil {
		return "", errors.New("invalid dashboard address")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || (u.Path != "" && u.Path != "/") || strings.ContainsAny(u.Host, "\\ \t\r\n") {
		return "", errors.New("invalid dashboard address")
	}
	u.Path = "/dashboard/"
	u.RawPath = ""
	return u.String(), nil
}
func probeDashboard(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if e != nil {
		return errors.New("dashboard unavailable")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("dashboard unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		return errors.New("dashboard unavailable")
	}
	return nil
}
func openDashboard(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", address)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", address)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", address)
	}
	return cmd.Run()
}
func runDashboard(ctx context.Context, address string, noOpen bool, out io.Writer, probe, open func(context.Context, string) error) int {
	fail := func(message string) int {
		r := management.Failure(routing.Fail("internal_error", message))
		r.Data, _ = json.Marshal(map[string]any{"url": address, "opened": false})
		_ = json.NewEncoder(out).Encode(r)
		return contracts.ErrorMapping("internal_error").Exit
	}
	if !noOpen {
		if probe(ctx, address) != nil {
			return fail("dashboard unavailable; start jev-router serve or check the instance URL")
		}
		if open(ctx, address) != nil {
			return fail("browser could not be opened; open the returned URL manually")
		}
	}
	_ = json.NewEncoder(out).Encode(management.Success(map[string]any{"url": address, "opened": !noOpen}, time.Now()))
	return 0
}
