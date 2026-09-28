package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
}
func TestConfigPrecedence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "conf.json")
	os.WriteFile(p, []byte(`{"listen":"127.0.0.1:9000","decision_timeout":"2s"}`), 0600)
	c, e := LoadConfig(p, env(map[string]string{"JEV_ROUTER_LISTEN": "127.0.0.1:9001"}))
	if e != nil || c.Listen != "127.0.0.1:9001" || c.DecisionTimeout != 2*time.Second {
		t.Fatalf("%+v %v", c, e)
	}
}
func TestDistinctRequiredTokens(t *testing.T) {
	c, _ := LoadConfig("", env(nil))
	for _, v := range []map[string]string{{}, {"JEV_ROUTER_SETTINGS_TOKEN": "same", "JEV_ROUTER_INFERENCE_TOKEN": "same"}} {
		if _, e := LoadCredentials(c, env(v), os.ReadFile); e == nil {
			t.Fatal("missing/identical credentials accepted")
		}
	}
}
func TestRoleIsolation(t *testing.T) {
	c, _ := LoadConfig("", env(nil))
	creds, e := LoadCredentials(c, env(map[string]string{"JEV_ROUTER_SETTINGS_TOKEN": "settings-secret", "JEV_ROUTER_INFERENCE_TOKEN": "inference-secret"}), os.ReadFile)
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"settings", "inference"} {
		p, e := Authenticate("Bearer "+role+"-secret", creds)
		if e != nil || p.Role != role {
			t.Fatal("role mismatch")
		}
	}
	if _, e := Authenticate("Bearer unknown", creds); e == nil {
		t.Fatal("invalid token accepted")
	}
}
func TestSecretNeverInErrors(t *testing.T) {
	secret := "unique-SECRET-marker"
	_, e := ResolveSecret("bad:"+secret, env(nil), os.ReadFile)
	if e == nil || strings.Contains(e.Error(), secret) {
		t.Fatal("secret exposed")
	}
	b, e := ResolveSecret("env:KEY", env(map[string]string{"KEY": secret}), os.ReadFile)
	if e != nil || string(b) != secret {
		t.Fatal("env resolution failed")
	}
}
func TestInvalidTimeoutRejected(t *testing.T) {
	if _, e := LoadConfig("", env(map[string]string{"JEV_ROUTER_IDLE_TIMEOUT": "0s"})); e == nil {
		t.Fatal("unbounded idle timeout")
	}
}
