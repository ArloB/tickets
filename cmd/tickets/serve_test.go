package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeGracefulShutdownDrainsInFlightRequest simulates a SIGTERM by
// cancelling serve's context directly (see serve's doc comment for why
// not a real OS signal) and confirms an in-flight request completes
// rather than being cut off, and that serve returns cleanly once it
// does.
func TestServeGracefulShutdownDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	logger := slog.New(slog.DiscardHandler)

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- serve(ctx, srv, ln, logger, 5*time.Second) }()

	reqDone := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			t.Errorf("GET /slow: %v", err)
			reqDone <- nil
			return
		}
		reqDone <- resp
	}()
	<-started // the handler is now genuinely in flight

	cancel()       // simulate SIGTERM
	close(release) // let the handler finish so Shutdown can complete

	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("serve returned %v, want nil (clean shutdown)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of context cancellation")
	}

	resp := <-reqDone
	if resp == nil {
		return // failure already reported above
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("in-flight request status = %d, want 200 (shutdown must drain it, not cut it off)", resp.StatusCode)
	}
}

// TestServeReturnsBindErrorImmediately confirms serve doesn't hang
// waiting for ctx cancellation when the listener itself is already
// dead — it should surface that error right away.
func TestServeReturnsBindErrorImmediately(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close() // closed listener: Serve must fail immediately

	srv := &http.Server{Handler: http.NewServeMux()}
	logger := slog.New(slog.DiscardHandler)

	err = serve(context.Background(), srv, ln, logger, time.Second)
	if err == nil {
		t.Fatal("serve on a closed listener: want error, got nil")
	}
}
