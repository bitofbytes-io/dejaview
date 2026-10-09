package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeFailsFastWhenPortInUse holds a port open and checks that serve
// returns the listen error instead of waiting for a shutdown signal.
func TestServeFailsFastWhenPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	httpServer := &http.Server{Addr: ln.Addr().String(), Handler: http.NotFoundHandler()}
	done := make(chan error, 1)
	go func() { done <- serve(context.Background(), httpServer) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serve returned nil, want a listen error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the listen failure")
	}
}

// TestServeShutsDownWhenContextDone checks that cancelling the signal context
// is still a clean shutdown.
func TestServeShutsDownWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	httpServer := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler()}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, httpServer) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the context was cancelled")
	}
}
