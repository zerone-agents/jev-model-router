package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestProviderStreamErrorCapturedFromActualFrame(t *testing.T) {
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
	if !errors.As(err, &upstream) || upstream.Details == nil {
		t.Fatalf("stream error: %#v", err)
	}
	b, _ := json.Marshal(upstream.Details)
	if !strings.Contains(string(b), "stream quota exceeded") {
		t.Fatal(string(b))
	}
}

func TestUnstructuredProviderErrorsAreSanitized(t *testing.T) {
	for _, body := range []string{"proxy Authorization: Bearer SECRET_SENTINEL", `{"message":"password=SECRET_SENTINEL"}`, `{"error":{"message":42}}`} {
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
			if !errors.As(err, &upstream) || upstream.Details == nil {
				t.Fatalf("missing upstream body: streaming=%v err=%v", streaming, err)
			}
			b, _ := json.Marshal(upstream.Details)
			if strings.Contains(string(b), "SECRET_SENTINEL") {
				t.Fatalf("secret leaked: %s", b)
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

func TestUpstreamCredentialRedaction(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(451)
				fmt.Fprint(w, `{"error":{"message":"region unavailable; echoed CUSTOM_CREDENTIAL","extra":{"api_key":"OTHER_SECRET"}},"request_id":"provider-123"}`)
			}))
			g, err := New(func(string) ([]byte, error) { return []byte("CUSTOM_CREDENTIAL"), nil })
			if err != nil {
				t.Fatal(err)
			}
			tg := target(server.URL)
			if native {
				tg = nativeTarget(server.URL)
			}
			if streaming {
				var s routing.EventStream
				s, err = g.Stream(context.Background(), tg, TestRequest())
				if err == nil {
					_, err = s.Next(context.Background())
					s.Close()
				}
			} else {
				_, err = g.Complete(context.Background(), tg, TestRequest())
			}
			b, _ := json.Marshal(routing.Diagnose(err))
			if !strings.Contains(string(b), "region unavailable") || !strings.Contains(string(b), "provider-123") || strings.Contains(string(b), "CUSTOM_CREDENTIAL") || strings.Contains(string(b), "OTHER_SECRET") {
				t.Fatalf("native=%v stream=%v: %s", native, streaming, b)
			}
			g.(interface{ Close() error }).Close()
			server.Close()
		}
	}
}

func TestStreamErrorFrameRedaction(t *testing.T) {
	for _, native := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if native {
				fmt.Fprint(w, "event: error\n")
			}
			fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"limited CUSTOM_CREDENTIAL\",\"api_key\":\"OTHER_SECRET\"}}\n\n")
		}))
		g, e := New(func(string) ([]byte, error) { return []byte("CUSTOM_CREDENTIAL"), nil })
		if e != nil {
			t.Fatal(e)
		}
		tg := target(server.URL)
		if native {
			tg = nativeTarget(server.URL)
		}
		stream, err := g.Stream(context.Background(), tg, TestRequest())
		if err == nil {
			_, err = stream.Next(context.Background())
			stream.Close()
		}
		d := routing.Diagnose(err)
		b, _ := json.Marshal(d)
		if d.UpstreamCode != "rate_limit_error" || !strings.Contains(string(b), "limited") || strings.Contains(string(b), "CUSTOM_CREDENTIAL") || strings.Contains(string(b), "OTHER_SECRET") {
			t.Fatalf("native=%v %s", native, b)
		}
		g.(interface{ Close() error }).Close()
		server.Close()
	}
}
