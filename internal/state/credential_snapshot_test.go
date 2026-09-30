package state

import (
	"context"
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/internal/provider"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCapturedTargetUsesOldCredentialAfterRotation(t *testing.T) {
	seen := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	s, e := OpenWithCipher(filepath.Join(t.TempDir(), "db"), time.Now, cipherForTest(t))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	create := managedCall(1, "p", "old-snapshot-key", "create")
	var in map[string]any
	json.Unmarshal(create.Input, &in)
	in["base_url"] = upstream.URL + "/v1"
	create.Input, _ = json.Marshal(in)
	if _, e = s.Apply(ctx, "settings", create, nil); e != nil {
		t.Fatal(e)
	}
	old, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	rotate := managedCall(2, "p", "new-snapshot-key", "rotate")
	json.Unmarshal(rotate.Input, &in)
	in["base_url"] = upstream.URL + "/v1"
	rotate.Input, _ = json.Marshal(in)
	if _, e = s.Apply(ctx, "settings", rotate, nil); e != nil {
		t.Fatal(e)
	}
	next, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	gen, e := provider.New(s.ResolveManaged)
	if e != nil {
		t.Fatal(e)
	}
	defer gen.(io.Closer).Close()
	model := routing.Model{ID: "m", UpstreamName: "gpt-4o-mini", Capabilities: routing.Capabilities{ContextLimit: 32000}}
	req := routing.Request{Model: "m", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	for _, tc := range []struct {
		p    routing.Provider
		want string
	}{{old.Providers[0], "Bearer old-snapshot-key"}, {next.Providers[0], "Bearer new-snapshot-key"}} {
		if _, e = gen.Complete(ctx, routing.Target{Provider: tc.p, Model: model}, req); e != nil {
			t.Fatal(e)
		}
		if got := <-seen; got != tc.want {
			t.Fatal("captured credential version changed")
		}
	}
}
