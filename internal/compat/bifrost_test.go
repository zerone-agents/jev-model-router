package compat

import (
	"context"
	"encoding/json"
	"fmt"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/providers/openai"
	"github.com/maximhq/bifrost/core/schemas"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type account map[schemas.ModelProvider]string

func (a account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	var p []schemas.ModelProvider
	for k := range a {
		p = append(p, k)
	}
	return p, nil
}
func (a account) GetKeysForProvider(context.Context, schemas.ModelProvider) ([]schemas.Key, error) {
	return []schemas.Key{{ID: "test", Value: *schemas.NewSecretVar("fake"), Models: schemas.WhiteList{"*"}, Weight: 1}}, nil
}
func (a account) GetConfigForProvider(p schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	return &schemas.ProviderConfig{NetworkConfig: schemas.NetworkConfig{BaseURL: a[p], MaxRetries: 0, DefaultRequestTimeoutInSeconds: 5}, ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{Concurrency: 2, BufferSize: 2}}, nil
}
func request(t *testing.T, ctx *schemas.BifrostContext, body string) *schemas.BifrostChatRequest {
	t.Helper()
	var r openai.OpenAIChatRequest
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	b := r.ToBifrostChatRequest(ctx)
	b.Provider = schemas.OpenAI
	b.Model = "gpt-4o-mini"
	b.Fallbacks = nil
	return b
}
func TestBifrostSelectionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		stream bool
	}{{"429", 429, false}, {"503", 503, false}, {"stream503", 503, true}, {"embedded", 200, true}, {"truncated", 200, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var selected, other atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selected.Add(1)
				if tc.name == "truncated" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
					return
				}
				if tc.status == 200 {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"error\":{\"message\":\"unavailable\",\"type\":\"server_error\"}}\n\n")
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"error":{"message":"unavailable"}}`)
			}))
			defer primary.Close()
			secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1); w.WriteHeader(500) }))
			defer secondary.Close()
			c, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account{schemas.OpenAI: primary.URL, schemas.Anthropic: secondary.URL}, Logger: bifrost.NewDefaultLogger(schemas.LogLevelError)})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Shutdown()
			ctx := schemas.NewBifrostContext(context.Background(), time.Now().Add(5*time.Second))
			defer ctx.Cancel()
			req := request(t, ctx, `{"messages":[{"role":"user","content":"test"}]}`)
			var failed bool
			if tc.stream {
				ch, e := c.ChatCompletionStreamRequest(ctx, req)
				failed = e != nil
				if ch != nil {
					for x := range ch {
						if x.BifrostError != nil {
							failed = true
						}
					}
				}
			} else {
				_, e := c.ChatCompletionRequest(ctx, req)
				failed = e != nil
			}
			if !failed {
				t.Fatal("upstream error lost")
			}
			if selected.Load() != 1 || other.Load() != 0 {
				t.Fatalf("calls %d/%d", selected.Load(), other.Load())
			}
		})
	}
}
func TestOpenAIFieldPreservation(t *testing.T) {
	ctx := schemas.NewBifrostContext(context.Background(), time.Now().Add(time.Second))
	defer ctx.Cancel()
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://example.invalid/image.png","detail":"low"}}]}],"max_completion_tokens":32,"temperature":0.2,"top_p":0.8,"stop":["END"],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}],"tool_choice":"required","parallel_tool_calls":false,"response_format":{"type":"json_object"},"stream_options":{"include_usage":true}}`
	req := request(t, ctx, body)
	encoded, err := json.Marshal(openai.ToOpenAIChatRequest(ctx, req))
	if err != nil {
		t.Fatal(err)
	}
	var original, got map[string]any
	json.Unmarshal([]byte(body), &original)
	json.Unmarshal(encoded, &got)
	for k, v := range original {
		if !reflect.DeepEqual(v, got[k]) {
			t.Errorf("field %s: got %v want %v", k, got[k], v)
		}
	}
}
func TestSmallTokenLimitCharacterization(t *testing.T) {
	ctx := schemas.NewBifrostContext(context.Background(), time.Now().Add(time.Second))
	defer ctx.Cancel()
	r := request(t, ctx, `{"messages":[{"role":"user","content":"hi"}],"max_completion_tokens":8}`)
	out := openai.ToOpenAIChatRequest(ctx, r)
	if *out.MaxCompletionTokens != 16 {
		t.Fatalf("SDK clamp changed: %d", *out.MaxCompletionTokens)
	}
}

func TestImageTransportNoFetch(t *testing.T) {
	var imageCalls atomic.Int32
	images := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { imageCalls.Add(1) }))
	defer images.Close()
	for _, image := range []string{images.URL + "/secret.png", "data:image/png;base64,aGVsbG8="} {
		t.Run(image[:4], func(t *testing.T) {
			var captured map[string]any
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if e := json.NewDecoder(r.Body).Decode(&captured); e != nil {
					t.Error(e)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"chat-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer upstream.Close()
			client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account{schemas.OpenAI: upstream.URL}, Logger: bifrost.NewDefaultLogger(schemas.LogLevelError)})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Shutdown()
			ctx := schemas.NewBifrostContext(context.Background(), time.Now().Add(5*time.Second))
			defer ctx.Cancel()
			raw := fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":%q}}]}]}`, image)
			_, failure := client.ChatCompletionRequest(ctx, request(t, ctx, raw))
			if failure != nil {
				t.Fatal(failure)
			}
			body, _ := json.Marshal(captured)
			if !containsJSONURL(body, image) {
				t.Fatalf("image changed: %s", body)
			}
			if imageCalls.Load() != 0 {
				t.Fatal("SDK fetched image")
			}
		})
	}
}
func containsJSONURL(body []byte, url string) bool {
	var v map[string]any
	json.Unmarshal(body, &v)
	messages, ok := v["messages"].([]any)
	if !ok || len(messages) != 1 {
		return false
	}
	parts, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(parts) != 1 {
		return false
	}
	img, ok := parts[0].(map[string]any)["image_url"].(map[string]any)
	return ok && img["url"] == url
}
