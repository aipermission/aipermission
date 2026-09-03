package connectors

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

type recordingNetworkTransport struct {
	requests chan NetworkDialRequest
	server   net.Conn
}

type failingNetworkTransport struct{ err error }

func (transport failingNetworkTransport) ConnectorRuntimeCapability() string {
	return NetworkTransportCapabilityName
}

func (transport failingNetworkTransport) DialConnectorTCP(context.Context, NetworkDialRequest) (net.Conn, error) {
	return nil, transport.err
}

func (transport *recordingNetworkTransport) ConnectorRuntimeCapability() string {
	return NetworkTransportCapabilityName
}

func (transport *recordingNetworkTransport) DialConnectorTCP(_ context.Context, request NetworkDialRequest) (net.Conn, error) {
	client, server := net.Pipe()
	transport.server = server
	transport.requests <- request
	return client, nil
}

func TestNewHTTPClientIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy-user:proxy-password@proxy.invalid:8080")
	t.Setenv("HTTPS_PROXY", "http://proxy-user:proxy-password@proxy.invalid:8080")
	t.Setenv("NO_PROXY", "")

	transport := &recordingNetworkTransport{requests: make(chan NetworkDialRequest, 1)}
	dialRequest := NetworkDialRequest{Mode: "over_ssh", Host: "service.internal", Port: 8080, TransportTargetRef: "ssh:1:1"}
	client := NewHTTPClient(transport, dialRequest, 2*time.Second)

	response := make(chan struct {
		requestURI         string
		proxyAuthorization string
		err                error
	}, 1)
	go func() {
		<-transport.requests
		defer transport.server.Close()
		request, err := http.ReadRequest(bufio.NewReader(transport.server))
		if err != nil {
			response <- struct {
				requestURI         string
				proxyAuthorization string
				err                error
			}{err: err}
			return
		}
		response <- struct {
			requestURI         string
			proxyAuthorization string
			err                error
		}{requestURI: request.RequestURI, proxyAuthorization: request.Header.Get("Proxy-Authorization")}
		_, _ = io.WriteString(transport.server, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	}()

	request, err := http.NewRequest(http.MethodGet, "http://service.internal:8080/health?full=true", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	result, err := client.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	result.Body.Close()

	got := <-response
	if got.err != nil {
		t.Fatalf("read request: %v", got.err)
	}
	if got.requestURI != "/health?full=true" {
		t.Fatalf("request URI = %q, want origin-form URI", got.requestURI)
	}
	if got.proxyAuthorization != "" {
		t.Fatalf("proxy authorization leaked to connector endpoint")
	}
}

func TestNewHTTPClientRefusesRedirects(t *testing.T) {
	client := NewHTTPClient(&recordingNetworkTransport{}, NetworkDialRequest{}, time.Second)
	request, err := http.NewRequest(http.MethodGet, "https://redirect.invalid", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := client.CheckRedirect(request, nil); err != http.ErrUseLastResponse {
		t.Fatalf("CheckRedirect() = %v, want http.ErrUseLastResponse", err)
	}
}

func TestTrackHTTPRequestDispatchDistinguishesDialFailureFromWrittenRequest(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "http://service.internal/mutate", nil)
	if err != nil {
		t.Fatal(err)
	}
	request, dispatched := TrackHTTPRequestDispatch(request)
	client := NewHTTPClient(failingNetworkTransport{err: errors.New("dial refused")}, NetworkDialRequest{}, time.Second)
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected dial failure")
	}
	if dispatched() {
		t.Fatal("dial failure was incorrectly marked as dispatched")
	}

	transport := &recordingNetworkTransport{requests: make(chan NetworkDialRequest, 1)}
	client = NewHTTPClient(transport, NetworkDialRequest{}, 2*time.Second)
	go func() {
		<-transport.requests
		defer transport.server.Close()
		_, _ = http.ReadRequest(bufio.NewReader(transport.server))
		_, _ = io.WriteString(transport.server, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	}()
	request, err = http.NewRequest(http.MethodPost, "http://service.internal/mutate", nil)
	if err != nil {
		t.Fatal(err)
	}
	request, dispatched = TrackHTTPRequestDispatch(request)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !dispatched() {
		t.Fatal("written request was not marked as dispatched")
	}
}
