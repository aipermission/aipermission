package connectors

import (
	"context"
	"net"
	"net/http"
	"time"
)

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
