package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/inference"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
)

func writePlaygroundStream(ctx context.Context, w http.ResponseWriter, s routing.EventStream, route inference.RouteMetadata) error {
	send := func(name string, value any) error { return sendPlaygroundEvent(w, name, value) }
	finish := ""
	var usage *routing.Usage
	fail := func(e error) error { send("error", playgroundDiagnostic(e, route.RequestID, "generation")); return e }
	for {
		event, e := s.Next(ctx)
		e = contextError(ctx, e)
		if e == io.EOF {
			if finish == "" {
				return fail(routing.Fail("upstream_error", "stream ended without completion"))
			}
			return send("done", struct {
				Finish string         `json:"finish_reason"`
				Usage  *routing.Usage `json:"usage,omitempty"`
			}{finish, usage})
		}
		if e != nil {
			return fail(e)
		}
		if event.Usage != nil {
			usage = event.Usage
		}
		for _, choice := range event.Choices {
			if choice.Index != 0 {
				return fail(routing.Fail("unsupported_request", "multiple choices are not supported"))
			}
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
				if finish != "stop" && finish != "length" && finish != "content_filter" {
					return fail(routing.Fail("unsupported_request", "unsupported playground completion"))
				}
			}
			d := choice.Delta
			if d == nil {
				continue
			}
			if len(d.ToolCalls) > 0 {
				return fail(routing.Fail("unsupported_request", "playground does not execute tools"))
			}
			delta := map[string]string{}
			if d.ReasoningContent != nil && *d.ReasoningContent != "" {
				delta["reasoning_content"] = *d.ReasoningContent
			}
			if len(d.Content) > 0 && string(d.Content) != "null" {
				var text string
				if json.Unmarshal(d.Content, &text) != nil {
					return fail(routing.Fail("unsupported_request", "non-text playground response"))
				}
				if text != "" {
					delta["content"] = text
				}
			}
			if d.Refusal != nil && *d.Refusal != "" {
				delta["content"] += *d.Refusal
			}
			if len(delta) > 0 {
				if e = send("delta", delta); e != nil {
					return e
				}
			}
		}
	}
}

func sendPlaygroundEvent(w http.ResponseWriter, name string, value any) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}

type playgroundDiagnosticBody struct {
	routing.Diagnostic
	RequestID string `json:"request_id"`
	Stage     string `json:"stage"`
}

func playgroundDiagnostic(e error, id, stage string) playgroundDiagnosticBody {
	return playgroundDiagnosticBody{routing.Diagnose(e), id, stage}
}
