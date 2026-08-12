package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeHTTPDrainsActiveRequestsBeforeReturning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		_, _ = io.WriteString(w, "done")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveHTTP(ctx, server, listener, time.Second) }()

	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
		responseDone <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach server")
	}
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("server returned before active request drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseRequest)
	if err := <-responseDone; err != nil {
		t.Fatalf("active request failed during shutdown: %v", err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("serve HTTP: %v", err)
	}
}

func TestServeHTTPBoundsShutdownAndClosesListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(requestStarted)
		<-releaseRequest
	})}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveHTTP(ctx, server, listener, 20*time.Millisecond) }()
	go func() {
		response, getErr := http.Get("http://" + listener.Addr().String())
		if getErr == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach server")
	}
	cancel()
	select {
	case err := <-serveDone:
		if err == nil {
			t.Fatal("timed-out shutdown returned no error")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown exceeded its bound")
	}
	close(releaseRequest)
}

func TestServeHTTPRunsRegisteredShutdownCallbacks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.NotFoundHandler()}
	cleanupCalled := make(chan struct{})
	server.RegisterOnShutdown(func() { close(cleanupCalled) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serveHTTP(ctx, server, listener, time.Second); err != nil {
		t.Fatalf("serve HTTP: %v", err)
	}
	select {
	case <-cleanupCalled:
	case <-time.After(time.Second):
		t.Fatal("registered shutdown callback was not called")
	}
}
