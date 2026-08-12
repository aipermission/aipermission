package api

import (
	"context"
	"net/http"
	"time"
)

const ordinaryRequestTimeout = 45 * time.Second
const remoteBrowseRequestTimeout = 75 * time.Second
const connectorActionRequestTimeout = 90 * time.Second

func withRequestDeadline(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if timeout <= 0 || isUnboundedRequestRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		requestTimeout := requestTimeoutForPath(r.URL.Path, timeout)
		deadline := time.Now().Add(requestTimeout)
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(deadline)
		_ = controller.SetWriteDeadline(deadline)
		defer func() {
			_ = controller.SetReadDeadline(time.Time{})
			_ = controller.SetWriteDeadline(time.Time{})
		}()
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestTimeoutForPath(path string, fallback time.Duration) time.Duration {
	switch path {
	case "/api/file-transfers/browse", "/api/file-transfers/expand":
		return remoteBrowseRequestTimeout
	case "/api/connector-actions/local-run", "/api/mcp/connector-actions/call":
		return connectorActionRequestTimeout
	default:
		return fallback
	}
}
