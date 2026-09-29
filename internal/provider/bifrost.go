package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"strings"
	"sync"
	"time"
)

type clientEntry struct {
	client *bifrost.Bifrost
	active int
	last   time.Time
}
type bifrostGenerator struct {
	resolve func(string) ([]byte, error)
	mu      sync.Mutex
	clients map[[32]byte]*clientEntry
	closed  bool
}

func New(resolve func(string) ([]byte, error)) (routing.Generator, error) {
	return &bifrostGenerator{resolve: resolve, clients: map[[32]byte]*clientEntry{}}, nil
}
func (g *bifrostGenerator) acquire(t routing.Target) (*bifrost.Bifrost, func(), error) {
	key, e := g.resolve(t.Provider.SecretRef)
	if e != nil {
		return nil, nil, routing.Fail("config_missing", "provider credential unavailable")
	}
	identity := sha256.Sum256(append([]byte(t.Provider.BaseURL+"\x00"), key...))
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, nil, routing.Fail("upstream_error", "generation service stopped")
	}
	entry := g.clients[identity]
	if entry == nil {
		if len(g.clients) >= 16 {
			var victim [32]byte
			var oldest *clientEntry
			for k, v := range g.clients {
				if v.active == 0 && (oldest == nil || v.last.Before(oldest.last)) {
					victim = k
					oldest = v
				}
			}
			if oldest == nil {
				return nil, nil, routing.Fail("upstream_error", "generation connection capacity reached")
			}
			delete(g.clients, victim)
			oldest.client.Shutdown()
		}
		c, e := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: fixedAccount{url: t.Provider.BaseURL, key: string(key)}, Logger: quietLogger{}, InitialPoolSize: 8})
		if e != nil {
			return nil, nil, routing.Fail("upstream_error", "generation initialization failed")
		}
		entry = &clientEntry{client: c}
		g.clients[identity] = entry
	}
	entry.active++
	return entry.client, func() { g.mu.Lock(); entry.active--; entry.last = time.Now(); g.mu.Unlock() }, nil
}
func (g *bifrostGenerator) Close() error {
	g.mu.Lock()
	g.closed = true
	clients := g.clients
	g.clients = map[[32]byte]*clientEntry{}
	g.mu.Unlock()
	for _, e := range clients {
		e.client.Shutdown()
	}
	return nil
}
func (g *bifrostGenerator) Complete(ctx context.Context, t routing.Target, r routing.Request) (routing.Completion, error) {
	if e := Check(t, r); e != nil {
		return routing.Completion{}, e
	}
	client, release, e := g.acquire(t)
	if e != nil {
		return routing.Completion{}, e
	}
	defer release()
	bc := schemas.NewBifrostContext(ctx, time.Time{})
	defer bc.Cancel()
	req, e := encode(bc, t, r)
	if e != nil {
		return routing.Completion{}, e
	}
	out, fail := client.ChatCompletionRequest(bc, req)
	if fail != nil {
		return routing.Completion{}, safeError(ctx)
	}
	return completion(out)
}
func (g *bifrostGenerator) Stream(ctx context.Context, t routing.Target, r routing.Request) (routing.EventStream, error) {
	if e := Check(t, r); e != nil {
		return nil, e
	}
	client, release, e := g.acquire(t)
	if e != nil {
		return nil, e
	}
	bc := schemas.NewBifrostContext(ctx, time.Time{})
	// Inspect original usage presence: the pinned SDK otherwise invents a zero
	// aggregate at stream completion. Raw frames stay inside this adapter.
	bc.SetValue(schemas.BifrostContextKeyAllowPerRequestRawOverride, true)
	bc.SetValue(schemas.BifrostContextKeySendBackRawResponse, true)
	req, e := encode(bc, t, r)
	if e != nil {
		bc.Cancel()
		release()
		return nil, e
	}
	ch, fail := client.ChatCompletionStreamRequest(bc, req)
	if fail != nil {
		bc.Cancel()
		release()
		return nil, safeError(ctx)
	}
	return &stream{ctx: bc, ch: ch, release: release}, nil
}

type stream struct {
	ctx           *schemas.BifrostContext
	ch            chan *schemas.BifrostStreamChunk
	release       func()
	once          sync.Once
	finished      bool
	reportedUsage bool
}

func (s *stream) Next(ctx context.Context) (routing.Event, error) {
	select {
	case <-ctx.Done():
		s.Close()
		return routing.Event{}, safeError(ctx)
	case <-s.ctx.Done():
		return routing.Event{}, safeError(s.ctx)
	case chunk, ok := <-s.ch:
		if !ok {
			if !s.finished {
				return routing.Event{}, routing.Fail("upstream_error", "upstream stream ended without completion")
			}
			return routing.Event{}, io.EOF
		}
		if chunk.BifrostError != nil {
			return routing.Event{}, safeError(s.ctx)
		}
		if chunk.BifrostChatResponse == nil {
			return routing.Event{}, routing.Fail("upstream_error", "invalid stream event")
		}
		s.reportedUsage = s.reportedUsage || hasReportedUsage(chunk.BifrostChatResponse.ExtraFields.RawResponse)
		out, e := completion(chunk.BifrostChatResponse)
		if !s.reportedUsage {
			out.Usage = nil
		}
		for _, c := range out.Choices {
			if c.FinishReason != nil {
				s.finished = true
			}
		}
		return out, e
	}
}
func (s *stream) Close() error { s.once.Do(func() { s.ctx.Cancel(); s.release() }); return nil }
func completion(in *schemas.BifrostChatResponse) (routing.Completion, error) {
	if in == nil {
		return routing.Completion{}, routing.Fail("upstream_error", "empty generation result")
	}
	safe := *in
	safe.ExtraFields = schemas.BifrostResponseExtraFields{}
	b, e := json.Marshal(&safe)
	if e != nil {
		return routing.Completion{}, routing.Fail("upstream_error", "invalid generation result")
	}
	var out routing.Completion
	if json.Unmarshal(b, &out) != nil {
		return out, routing.Fail("upstream_error", "invalid generation result")
	}
	// Historical assistant tool calls use array order; index belongs only to
	// streaming deltas. Strip the SDK-added index from complete messages.
	for _, choice := range out.Choices {
		if choice.Message != nil {
			for i := range choice.Message.ToolCalls {
				choice.Message.ToolCalls[i].Index = nil
			}
		}
	}
	if in.Usage != nil {
		u := in.Usage
		out.Usage = &routing.Usage{InputTokens: int64(u.PromptTokens), OutputTokens: int64(u.CompletionTokens), TotalTokens: int64(u.TotalTokens)}
		if u.PromptTokensDetails != nil {
			out.Usage.InputDetails, _ = json.Marshal(u.PromptTokensDetails)
		}
		if u.CompletionTokensDetails != nil {
			out.Usage.OutputDetails, _ = json.Marshal(u.CompletionTokensDetails)
		}
	}
	return out, nil
}
func safeError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return routing.Fail("timeout", "upstream request timed out")
	}
	if ctx.Err() != nil {
		return routing.Fail("cancelled", "request cancelled")
	}
	return routing.Fail("upstream_error", "generation provider failed")
}

type fixedAccount struct{ url, key string }

func (a fixedAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{schemas.OpenAI}, nil
}
func (a fixedAccount) GetKeysForProvider(context.Context, schemas.ModelProvider) ([]schemas.Key, error) {
	return []schemas.Key{{ID: "selected", Value: *schemas.NewSecretVar(a.key), Models: schemas.WhiteList{"*"}, Weight: 1}}, nil
}
func (a fixedAccount) GetConfigForProvider(schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	// Provider BaseURL already includes the compatible API prefix.
	return &schemas.ProviderConfig{CustomProviderConfig: &schemas.CustomProviderConfig{
		BaseProviderType: schemas.OpenAI,
		RequestPathOverrides: map[schemas.RequestType]string{
			schemas.ChatCompletionRequest:       "/chat/completions",
			schemas.ChatCompletionStreamRequest: "/chat/completions",
		},
	}, NetworkConfig: schemas.NetworkConfig{BaseURL: a.url, MaxRetries: 0, DefaultRequestTimeoutInSeconds: 300}, ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{Concurrency: 8, BufferSize: 8}}, nil
}

type quietLogger struct{}

func (quietLogger) Debug(string, ...any)                   {}
func (quietLogger) Info(string, ...any)                    {}
func (quietLogger) Warn(string, ...any)                    {}
func (quietLogger) Error(string, ...any)                   {}
func (quietLogger) Fatal(string, ...any)                   {}
func (quietLogger) SetLevel(schemas.LogLevel)              {}
func (quietLogger) SetOutputType(schemas.LoggerOutputType) {}
func (quietLogger) LogHTTPRequest(schemas.LogLevel, string) schemas.LogEventBuilder {
	return schemas.NoopLogEvent
}

// Captured SSE payloads are consecutive JSON values separated by whitespace.
// A genuine reported zero object is distinct from an absent or null usage.
func hasReportedUsage(raw any) bool {
	text, ok := raw.(string)
	if !ok {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	for {
		var frame struct {
			Usage *json.RawMessage `json:"usage"`
		}
		if decoder.Decode(&frame) != nil {
			return false
		}
		if frame.Usage != nil {
			return true
		}
	}
}
