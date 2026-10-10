package provider

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderErrorDetails(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"quota exceeded","type":"rate_limit_error","code":"quota","param":"model"}}`)
		}))
		g := generator(t)
		var err error
		if streaming {
			stream, e := g.Stream(context.Background(), target(server.URL), TestRequest())
			err = e
			if err == nil {
				_, err = stream.Next(context.Background())
				stream.Close()
			}
		} else {
			_, err = g.Complete(context.Background(), target(server.URL), TestRequest())
		}
		var upstream *routing.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != 429 || upstream.Body["message"] != "quota exceeded" {
			t.Fatalf("stream=%v error=%#v", streaming, err)
		}
		if d := routing.Diagnose(err); d.UpstreamCode != "rate_limit_error" {
			t.Fatalf("reported code: %+v", d)
		}
		server.Close()
	}
}

func TestProviderStreamErrorWithoutProvenanceStaysPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"stream quota exceeded\",\"type\":\"rate_limit_error\",\"code\":\"quota\"}}\n\n")
	}))
	defer server.Close()
	stream, err := generator(t).Stream(context.Background(), target(server.URL), TestRequest())
	if err == nil {
		defer stream.Close()
		_, err = stream.Next(context.Background())
	}
	var upstream *routing.UpstreamError
	if err == nil || errors.As(err, &upstream) {
		t.Fatalf("stream error: %#v", err)
	}
}

func TestUnstructuredProviderErrorsStayPrivate(t *testing.T) {
	for _, body := range []string{"proxy Authorization: Bearer SECRET_SENTINEL", `{"message":"SECRET_SENTINEL"}`, `{"error":{"message":42}}`} {
		for _, streaming := range []bool{false, true} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502); fmt.Fprint(w, body) }))
			g := generator(t)
			var err error
			if streaming {
				stream, e := g.Stream(context.Background(), target(server.URL), TestRequest())
				err = e
				if err == nil {
					_, err = stream.Next(context.Background())
					stream.Close()
				}
			} else {
				_, err = g.Complete(context.Background(), target(server.URL), TestRequest())
			}
			server.Close()
			var upstream *routing.UpstreamError
			if err == nil || errors.As(err, &upstream) {
				t.Fatalf("unstructured error entered passthrough: streaming=%v", streaming)
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	_, err := generator(t).Complete(context.Background(), target(server.URL), TestRequest())
	var upstream *routing.UpstreamError
	if err == nil || errors.As(err, &upstream) {
		t.Fatal("connection failure entered passthrough")
	}
}
