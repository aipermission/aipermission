package connectors

import (
	"bufio"
	"context"
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
