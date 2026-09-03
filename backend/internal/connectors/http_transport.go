package connectors

import (
	"context"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

// TrackHTTPRequestDispatch returns a request whose trace records whether the
// transport attempted to write the request. A failed or partial write can still
// reach the remote service, so the signal distinguishes only failures known to
// occur before dispatch from failures whose remote outcome may be unknown.
func TrackHTTPRequestDispatch(request *http.Request) (*http.Request, func() bool) {
	var dispatched atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			dispatched.Store(true)
		},
	}
	return request.WithContext(httptrace.WithClientTrace(request.Context(), trace)), dispatched.Load
}

// NewHTTPClient returns an HTTP client whose sockets are opened exclusively by
// the connector network transport. Environment proxy settings are deliberately
// ignored so connector credentials cannot be redirected to an ambient proxy.
func NewHTTPClient(transport NetworkTransport, request NetworkDialRequest, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return transport.DialConnectorTCP(ctx, request)
			},
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}
