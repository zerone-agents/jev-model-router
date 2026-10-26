package decision

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerone-agents/jev-model-router/internal/routing"
)

// A peer that accepts TCP but never completes TLS reproduces a transport
// timeout while the configured overall decision deadline is still live.
func TestDecisionTLSHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-done
	}()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = 20 * time.Millisecond
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = New(&http.Client{Transport: transport}, func(string) ([]byte, error) { return []byte("test-key"), nil }, DefaultBudget()).Choose(ctx, routing.DecisionConfig{BaseURL: "https://" + listener.Addr().String()}, input())
	if ctx.Err() != nil {
		t.Fatalf("outer decision deadline expired: %v", ctx.Err())
	}
	if d := routing.Diagnose(err); d.Code != "timeout" {
		t.Fatalf("transport timeout classified as %q; want timeout (err=%v)", d.Code, err)
	}
}

type delayedTLSListener struct {
	net.Listener
	delay time.Duration
}

func (l delayedTLSListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &delayedTLSConn{Conn: c, delay: l.delay}, nil
}

type delayedTLSConn struct {
	net.Conn
	delay time.Duration
	once  sync.Once
}

func (c *delayedTLSConn) Read(b []byte) (int, error) {
	c.once.Do(func() { time.Sleep(c.delay) })
	return c.Conn.Read(b)
}

func TestDefaultDecisionTransportUsesDecisionDeadline(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"answers":{"model":{"type":"choice","choice":"m0"}}}`))
	}))
	server.Listener = delayedTLSListener{server.Listener, 80 * time.Millisecond}
	server.StartTLS()
	defer server.Close()
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSHandshakeTimeout = 20 * time.Millisecond
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original; transport.CloseIdleConnections() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d, err := New(nil, func(string) ([]byte, error) { return []byte("test"), nil }, DefaultBudget()).Choose(ctx, routing.DecisionConfig{BaseURL: server.URL}, input())
	if err != nil || d.ModelID != "fast" {
		t.Fatalf("handshake within decision budget failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d; expected no retry", calls.Load())
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	_, err = New(nil, func(string) ([]byte, error) { return []byte("test"), nil }, DefaultBudget()).Choose(ctx2, routing.DecisionConfig{BaseURL: server.URL}, input())
	if routing.Diagnose(err).Code != "timeout" {
		t.Fatalf("decision deadline not enforced: %v", err)
	}
}
