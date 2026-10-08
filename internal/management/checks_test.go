package management_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
	"testing"
	"time"
)

type memoryStore struct{ s routing.Snapshot }

func (m *memoryStore) Snapshot(context.Context) (routing.Snapshot, error) { return m.s, nil }
func (m *memoryStore) Apply(context.Context, string, management.Call, func(routing.Snapshot) error) (management.Result, error) {
	panic("unexpected mutation")
}

type generator struct{ calls int }

func (g *generator) Complete(_ context.Context, _ routing.Target, r routing.Request) (routing.Completion, error) {
	g.calls++
	if string(r.Messages[0].Content) != `"Reply OK."` || string(r.Options["max_completion_tokens"]) != "16" {
		panic("not fixed probe")
	}
	return routing.Completion{}, nil
}
func (g *generator) Stream(context.Context, routing.Target, routing.Request) (routing.EventStream, error) {
	panic("stream probe")
}
func checksFixture() (*management.Checks, *memoryStore, *generator) {
	s := &memoryStore{routing.Snapshot{Version: 1, Models: []routing.Model{{ID: "m", ProviderID: "p", UpstreamName: "gpt-4o-mini", Capabilities: routing.Capabilities{ContextLimit: 10000}}}, Providers: []routing.Provider{{ID: "p"}}}}
	g := &generator{}
	return &management.Checks{Store: s, Planner: &routing.Planner{}, Generator: g, FirstEventTimeout: time.Second}, s, g
}
func TestSettingsCanTestDisabledButNotChat(t *testing.T) {
	c, s, g := checksFixture()
	r, e := c.TestModel(context.Background(), "m")
	if e != nil || !r.OK || g.calls != 1 || s.s.Models[0].Enabled || s.s.Version != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	service := management.New(s, nil)
	c.Register(service)
	_, e = service.Execute(context.Background(), "settings", management.Call{CapabilityID: "models.test", Input: json.RawMessage(`{"id":"m","messages":[]}`)})
	if e == nil || g.calls != 1 {
		t.Fatal("arbitrary probe accepted")
	}
}
func TestInspectionDoesNotGenerateOrMutate(t *testing.T) {
	c, s, g := checksFixture()
	s.s.Models[0].Enabled = true
	p, e := c.Inspect(context.Background(), routing.Request{Model: "auto", Messages: []routing.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}})
	if e != nil || p.ModelID != "m" || g.calls != 0 || s.s.Version != 1 {
		t.Fatalf("%+v %v", p, e)
	}
}

func TestProviderCredentialPreview(t *testing.T) {
	for _, tc := range []struct{ key, want string }{{"sk-example-secret", `"sk-e***"`}, {"short", `"***"`}, {"", "null"}} {
		t.Run(tc.key, func(t *testing.T) {
			store := &memoryStore{routing.Snapshot{Version: 1, Providers: []routing.Provider{{ID: "p", BaseURL: "https://example.com/v1", SecretRef: "env:KEY"}}}}
			service := management.New(store, nil)
			service.ResolveSecret = func(ref string) ([]byte, error) {
				if ref != "env:KEY" {
					t.Fatal(ref)
				}
				return []byte(tc.key), nil
			}
			result, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "providers.get", Input: json.RawMessage(`{"id":"p"}`)})
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}
			if string(data["api_key_masked"]) != tc.want {
				t.Fatalf("unexpected masked key: %s", data["api_key_masked"])
			}
		})
	}
}

func TestUnavailableCredentialWarning(t *testing.T) {
	store := &memoryStore{routing.Snapshot{Version: 1, Providers: []routing.Provider{{ID: "p", SecretRef: "env:KEY"}}}}
	service := management.New(store, nil)
	service.ResolveSecret = func(string) ([]byte, error) { return nil, fmt.Errorf("sensitive-upstream-error") }
	r, e := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "providers.get", Input: json.RawMessage(`{"id":"p"}`)})
	if e != nil || len(r.Warnings) != 1 || r.Warnings[0] != "provider_credential_unavailable" {
		t.Fatal("missing generic warning")
	}
}

func TestInspectionFieldDiagnostics(t *testing.T) {
	checks, store, generator := checksFixture()
	store.s.Models[0].Enabled = true
	service := management.New(store, nil)
	checks.Register(service)
	for _, tc := range []struct{ fields, code, message string }{
		{`"max_tokens":1024,"max_completion_tokens":1024`, "invalid_request", "mutually exclusive"},
		{`"max_tokens":null`, "invalid_request", "max_tokens must be an integer between 16 and 10000000"},
		{`"max_tokens":10000001`, "invalid_request", "max_tokens must be an integer between 16 and 10000000"},
		{`"max_completion_tokens":15`, "invalid_request", "max_completion_tokens must be an integer between 16 and 10000000"},
		{`"reasoning_effort":"SECRET"`, "unsupported_request", "reasoning_effort"},
	} {
		input := json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"SECRET"}],` + tc.fields + `}`)
		_, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "route.inspect", Input: input})
		result := management.Failure(err)
		if result.Error.Code != tc.code || !strings.Contains(result.Error.Message, tc.message) || strings.Contains(result.Error.Message, "SECRET") {
			t.Fatalf("%s: %+v", tc.fields, result.Error)
		}
	}
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		input := json.RawMessage(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"` + field + `":1024}`)
		result, err := service.Execute(context.Background(), "settings", management.Call{CapabilityID: "route.inspect", Input: input})
		if err != nil || !result.OK {
			t.Fatalf("%s: %+v %v", field, result, err)
		}
	}
	if generator.calls != 0 || store.s.Version != 1 {
		t.Fatal("inspection generated or mutated configuration")
	}
}

func TestProviderProtocolEndpoint(t *testing.T) {
	for _, tc := range []struct {
		p    routing.ProviderProtocol
		path string
	}{{routing.ProtocolAnthropic, "/messages"}, {routing.ProtocolOpenAI, "/chat/completions"}} {
		store := &memoryStore{routing.Snapshot{Providers: []routing.Provider{{ID: "p", Protocol: tc.p, BaseURL: "https://example.com/v1/"}}}}
		result, e := management.New(store, nil).Execute(context.Background(), "settings", management.Call{CapabilityID: "providers.get", Input: json.RawMessage(`{"id":"p"}`)})
		if e != nil {
			t.Fatal(e)
		}
		var d struct{ Endpoint string }
		json.Unmarshal(result.Data, &d)
		if d.Endpoint != "https://example.com/v1"+tc.path {
			t.Fatalf("endpoint %q", d.Endpoint)
		}
	}
}
