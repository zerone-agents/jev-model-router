package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func request(ctx context.Context, base, method, path string, token []byte, body any) (management.Result, error) {
	var result management.Result
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return result, routing.Fail("invalid_request", "invalid instance URL")
	}
	if e = validateSecretTransport(u, body); e != nil {
		return result, e
	}
	var b []byte
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return result, routing.Fail("invalid_request", "invalid call")
		}
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(b))
	if e != nil {
		return result, routing.Fail("invalid_request", "invalid capability path")
	}
	r.Header.Set("Authorization", "Bearer "+string(token))
	r.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return result, routing.Fail("upstream_error", "instance request failed; outcome may be unknown")
	}
	defer resp.Body.Close()
	b, e = io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if e != nil || len(b) > 16<<20 || json.Unmarshal(b, &result) != nil || (!result.OK && result.Error == nil) {
		return result, routing.Fail("upstream_error", "invalid instance response")
	}
	return result, nil
}

// Capability ID is in the request path; protect inline secret fields regardless
// of whether the caller populated Call.CapabilityID.
func validateSecretTransport(base *url.URL, body any) error {
	call, ok := body.(management.Call)
	if !ok {
		return nil
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(call.Input, &input) != nil {
		return nil
	}
	if _, sensitive := input["api_key"]; !sensitive {
		return nil
	}
	host := base.Hostname()
	if base.Scheme == "https" || host == "localhost" || net.ParseIP(host).IsLoopback() {
		return nil
	}
	return routing.Fail("invalid_request", "inline credentials require HTTPS or loopback HTTP")
}
