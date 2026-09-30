package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEncryptionConfigAndLoading(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(p, []byte(strings.Repeat("ab", 32)+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := LoadConfig("", env(map[string]string{"JEV_ROUTER_ENCRYPTION_KEY_REF": "file:" + p}))
	if e != nil || cfg.EncryptionKeyRef != "file:"+p {
		t.Fatal("missing config", e)
	}
	t.Setenv("TEST_MASTER", strings.Repeat("ab", 32))
	t.Setenv("TEST_EMPTY", "")
	t.Setenv("TEST_BAD", "not-a-key")
	for _, ref := range []string{"", "env:TEST_EMPTY", "env:TEST_MASTER", "file:" + p} {
		c, e := loadEncryptionCipher(ref)
		if e != nil {
			t.Fatal(e)
		}
		if (ref == "" || ref == "env:TEST_EMPTY") != (c == nil) {
			t.Fatal("incorrect optional key")
		}
	}
	for _, ref := range []string{"env:TEST_BAD", "managed:bad", "file:/no/such/key"} {
		if _, e := loadEncryptionCipher(ref); e == nil {
			t.Fatal("invalid encryption config accepted")
		}
	}
}
func TestManagedHandlerWriteAndRestart(t *testing.T) {
	t.Setenv("JEV_ROUTER_SETTINGS_TOKEN", "settings-test")
	t.Setenv("JEV_ROUTER_INFERENCE_TOKEN", "inference-test")
	t.Setenv("TEST_MASTER", strings.Repeat("ab", 32))
	cfg, e := LoadConfig("", env(nil))
	if e != nil {
		t.Fatal(e)
	}
	cfg.Database = filepath.Join(t.TempDir(), "db")
	cfg.EncryptionKeyRef = "env:TEST_MASTER"
	h, closeStore, e := Handler(cfg)
	if e != nil {
		t.Fatal(e)
	}
	body := `{"input":{"id":"p","base_url":"https://example.com/v1","api_key":"sk-test-secret-marker"},"expected_version":1,"idempotency_key":"create"}`
	req := httptest.NewRequest("POST", "/admin/v1/call/providers.put", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer settings-test")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("transaction validation deadlocked")
	}
	if w.Code != 200 || strings.Contains(w.Body.String(), "sk-test-secret-marker") {
		t.Fatal("write failed/leaked", w.Code, w.Body.String())
	}
	closeStore()
	h, closeStore, e = Handler(cfg)
	if e != nil {
		t.Fatal(e)
	}
	req = httptest.NewRequest("POST", "/admin/v1/call/providers.get", strings.NewReader(`{"input":{"id":"p"}}`))
	req.Header.Set("Authorization", "Bearer settings-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var result struct {
		Data struct {
			Masked string `json:"api_key_masked"`
		}
	}
	if e = json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&result); e != nil || result.Data.Masked != "sk-t***" {
		t.Fatal("mask unavailable after restart", e)
	}
	closeStore()
	for _, key := range []string{"", strings.Repeat("cd", 32), "invalid"} {
		t.Setenv("TEST_MASTER", key)
		if _, cleanup, e := Handler(cfg); e == nil {
			cleanup()
			t.Fatal("wrong or missing master accepted")
		}
	}
}
