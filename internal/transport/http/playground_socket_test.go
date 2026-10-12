package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaygroundSlowSocketDeadline(t *testing.T) {
	l := playground.DefaultLimits()
	l.Timeout = 300 * time.Millisecond
	started := make(chan context.Context, 1)
	g := &pgGenerator{t: t, stream: func(ctx context.Context) (routing.EventStream, error) {
		started <- ctx
		return &testStream{next: func(ctx context.Context) (routing.Event, error) {
			return routing.Event{Choices: []routing.Choice{{Delta: &routing.Delta{Content: json.RawMessage(`"` + strings.Repeat("x", 131072) + `"`)}}}}, nil
		}}, nil
	}}
	h, token, csrf, svc := pgFixture(t, l, g)
	ended := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r); close(ended) }))
	defer srv.Close()
	conn, e := net.Dial("tcp", srv.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if e := conn.(*net.TCPConn).SetReadBuffer(1024); e != nil {
		t.Fatal(e)
	}
	_, e = fmt.Fprintf(conn, "POST /admin/v1/playground/completions HTTP/1.1\r\nHost: localhost\r\nOrigin: http://localhost\r\nContent-Type: application/json\r\nX-CSRF-Token: %s\r\nCookie: jev_router_session=%s\r\nContent-Length: %d\r\n\r\n%s", csrf, token, len(pgInput), pgInput)
	if e != nil {
		t.Fatal(e)
	}
	var ctx context.Context
	select {
	case ctx = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("not started")
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("slow socket did not terminate")
	}
	if ctx.Err() == nil {
		t.Fatal("upstream not cancelled")
	}
	r := httptest.NewRequest("GET", "http://localhost/admin/v1/playground", nil)
	r.Header.Set("X-Jev-Session", "1")
	r.AddCookie(&http.Cookie{Name: "jev_router_session", Value: token})
	id, e := h.(*playgroundHTTP).sessions.AuthenticatePlayground(r)
	if e != nil {
		t.Fatal(e)
	}
	lease, e := svc.Acquire(context.Background(), id)
	if e != nil {
		t.Fatal("slot leaked", e)
	}
	lease.Release()
}
