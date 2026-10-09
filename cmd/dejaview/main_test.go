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
// shuts down a running server cleanly: serve returns nil and the server stops
// accepting requests.
func TestServeShutsDownWhenContextDone(t *testing.T) {
	// Pick a free port, then release it for serve to listen on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	httpServer := &http.Server{Addr: addr, Handler: handler}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, httpServer) }()

	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	url := "http://" + addr + "/"

	// Wait until the server answers so the cancel below shuts down a live server.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
			}
			break
		}
		select {
		case serveErr := <-done:
			t.Fatalf("serve returned %v before the server was ready", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server not ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the context was cancelled")
	}

	if resp, err := client.Get(url); err == nil {
		resp.Body.Close()
		t.Fatal("request succeeded after shutdown, want it to fail")
	}
}
