package httptransport

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerone-agents/jev-model-router/internal/playground"
)

func TestPlaygroundPublicChatNativeHistoryParity(t *testing.T) {
	public, service, executor := nativeChatFixture(t)
	fixture, token, csrf, _ := pgFixture(t, playground.DefaultLimits(), &pgGenerator{t: t})
	pg := fixture.(*playgroundHTTP)
	pg.inference, pg.executor = service, executor
	for _, model := range []string{"auto", "external"} {
		for _, messages := range []string{
			`[{"role":"user","content":"hi"}]`,
			`[{"role":"user","content":"hi"},{"role":"assistant","content":"answer","reasoning_content":"thought"},{"role":"user","content":"parity followup"}]`,
		} {
			body := fmt.Sprintf(`{"model":%q,"messages":%s,"stream":true}`, model, messages)
			api := httptest.NewRecorder()
			public.ServeHTTP(api, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
			ui := sessionRequest(pg, "POST", "/admin/v1/playground/completions", body, token, csrf)
			for _, result := range []*httptest.ResponseRecorder{api, ui} {
				if result.Code != 200 || !strings.Contains(result.Body.String(), "thought") || !strings.Contains(result.Body.String(), "answer") {
					t.Fatalf("stream parity: %d %s", result.Code, result.Body.String())
				}
			}
			if !strings.Contains(api.Body.String(), "[DONE]") || !strings.Contains(ui.Body.String(), "event: done") {
				t.Fatal("missing successful terminal")
			}
		}
	}
	// Candidate rejection is identical and occurs before either wire stream starts.
	body := `{"model":"missing","messages":[{"role":"user","content":"hi"}],"stream":true}`
	api := httptest.NewRecorder()
	public.ServeHTTP(api, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	ui := sessionRequest(pg, "POST", "/admin/v1/playground/completions", body, token, csrf)
	if api.Code != ui.Code || !strings.Contains(ui.Body.String(), "not_found") {
		t.Fatalf("error parity: %d %s / %d %s", api.Code, api.Body, ui.Code, ui.Body)
	}
}
