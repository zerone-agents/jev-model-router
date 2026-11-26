package app_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/app"
	"github.com/zerone-agents/jev-model-router/internal/transport/cli"
)

func TestFailureCaptureThroughManagementHTTP(t *testing.T) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "inference-test")
	cfg, err := app.LoadConfig("", os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RecordMaxCount = 1
	cfg.Database = filepath.Join(t.TempDir(), "db")
	handler, cleanup, err := app.Handler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	failed := request("POST", "/v1/chat/completions", "inference-test",
		`{"model":"auto","max_tokens":1,"messages":[{"role":"user","content":"private-third-party-token"}]}`)
	if failed.Code != 400 {
		t.Fatalf("expected validation failure, got %d: %s", failed.Code, failed.Body)
	}
	id := failed.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("missing request ID")
	}
	input, _ := json.Marshal(map[string]any{"input": map[string]string{"request_id": id}})
	for _, capability := range []string{"list", "get", "delete"} {
		response := request("GET", "/admin/v1/schema/records.capture."+capability, "settings-test", "")
		if response.Code != 200 {
			t.Fatalf("discovery %s: %d %s", capability, response.Code, response.Body)
		}
	}
	getPath := "/admin/v1/call/records.capture.get"
	if denied := request("POST", getPath, "inference-test", string(input)); denied.Code != 403 {
		t.Fatalf("inference credential can access capture: %d", denied.Code)
	}
	got := request("POST", getPath, "settings-test", string(input))
	if got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(got.Body.String(), `"max_tokens":1`) ||
		strings.Contains(got.Body.String(), "private-third-party-token") {
		t.Fatalf("unexpected capture: %d %s", got.Code, got.Body)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("JEV_ROUTER_URL", server.URL)
	runCLI := func(input string, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := cli.Run(context.Background(), args, strings.NewReader(input), &stdout, &stderr); code != 0 {
			t.Fatalf("CLI %v exited %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}
	if schema := runCLI("", "schema", "records.capture.get"); !strings.Contains(schema, "request_id") {
		t.Fatalf("CLI discovery missing request_id: %s", schema)
	}
	if captures := runCLI("{}", "call", "records.capture.list", "--json", "-"); !strings.Contains(captures, id) || !strings.Contains(captures, `"error_code":"invalid_request"`) {
		t.Fatalf("CLI list missing request: %s", captures)
	}
	cliInput, _ := json.Marshal(map[string]string{"request_id": id})
	export := runCLI(string(cliInput), "call", "records.capture.get", "--json", "-")
	if !json.Valid([]byte(export)) || !strings.Contains(export, id) ||
		!strings.Contains(export, "max_tokens") || strings.Contains(export, "private-third-party-token") {
		t.Fatalf("invalid CLI export: %s", export)
	}
	list := request("POST", "/admin/v1/call/records.list", "settings-test", `{"input":{}}`)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"failure_capture_available":true`) {
		t.Fatalf("missing record marker: %d %s", list.Code, list.Body)
	}
	raw := `{"model":"auto","messages":[{"role":"user","content":"` + strings.Repeat("x", 64<<10) + `"},{"role":"user","content":[{"type":null}]}]} ` + "\n"
	oversized := request("POST", "/v1/chat/completions", "inference-test", raw)
	if oversized.Code != 400 || !strings.Contains(oversized.Body.String(), "messages[1].content[0].type") {
		t.Fatalf("missing nested HTTP diagnostic: %d %s", oversized.Code, oversized.Body)
	}
	nestedInput, _ := json.Marshal(map[string]string{"request_id": oversized.Header().Get("X-Request-ID")})
	nested := runCLI(string(nestedInput), "call", "records.capture.get", "--json", "-")
	if !strings.Contains(nested, "messages[1].content[0].type") || !strings.Contains(nested, `"truncated":true`) {
		t.Fatalf("missing independent capture diagnostic: %s", nested)
	}

	var original struct {
		Data struct {
			OriginalRequests struct {
				Client struct {
					State string
					Bytes string `json:"bytes_base64"`
				}
			} `json:"original_requests"`
		}
	}
	if err := json.Unmarshal([]byte(nested), &original); err != nil {
		t.Fatal(err)
	}
	body, err := base64.StdEncoding.DecodeString(original.Data.OriginalRequests.Client.Bytes)
	if err != nil || string(body) != raw || original.Data.OriginalRequests.Client.State != "complete" {
		t.Fatal("original client bytes did not round-trip")
	}

	second := request("POST", "/v1/chat/completions", "inference-test", `{"model":"auto","max_tokens":1}`)
	if second.Code != 400 || second.Header().Get("X-Request-ID") == id {
		t.Fatalf("failed to generate another routing record: %d %s", second.Code, second.Body)
	}
	records := runCLI("{}", "call", "records.list", "--json", "-")
	if strings.Contains(records, id) {
		t.Fatalf("ordinary record was not evicted: %s", records)
	}
	captures := runCLI("{}", "call", "records.capture.list", "--json", "-")
	if !strings.Contains(captures, id) || !strings.Contains(captures, `"error_code":"invalid_request"`) {
		t.Fatalf("capture lost after ordinary record eviction: %s", captures)
	}
	runCLI(string(cliInput), "call", "records.capture.get", "--json", "-")
	runCLI(string(cliInput), "call", "records.capture.delete", "--json", "-")
	for range 2 {
		deleted := request("POST", "/admin/v1/call/records.capture.delete", "settings-test", string(input))
		if deleted.Code != 200 {
			t.Fatalf("delete: %d %s", deleted.Code, deleted.Body)
		}
	}
	if missing := request("POST", getPath, "settings-test", string(input)); missing.Code != 404 {
		t.Fatalf("deleted capture remains available: %d", missing.Code)
	}
}
