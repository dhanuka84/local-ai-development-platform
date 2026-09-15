package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeDrainsActiveRequestsBeforeReturning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	shutdownStarted := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})}
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, server, listener, 5*time.Second) }()

	requested := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			err = response.Body.Close()
		}
		requested <- err
	}()
	select {
	case <-started:
	case err := <-requested:
		t.Fatalf("request ended before handler started: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	<-shutdownStarted
	select {
	case err := <-served:
		t.Fatalf("Serve returned while a handler was active: %v", err)
	default:
	}
	release <- struct{}{}
	if err := <-requested; err != nil {
		t.Fatalf("drained request: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve after cancellation: %v", err)
	}
}

func TestServeReportsShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(finished)
	})}
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, server, listener, time.Millisecond) }()
	requested := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			err = response.Body.Close()
		}
		requested <- err
	}()
	select {
	case <-started:
	case err := <-requested:
		t.Fatalf("request ended before handler started: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	if err := <-served; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve error = %v; want shutdown deadline exceeded", err)
	}
	<-finished
	if err := <-requested; err == nil {
		t.Fatal("request succeeded after forced connection close")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Serve(t.Context(), &http.Server{}, listener, time.Second); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Serve error = %v; want closed listener", err)
	}
}
