// Package gatewayhttp owns the local-only browser, lifecycle, and timeout
// boundary applied around gateway HTTP routes.
package gatewayhttp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	OrdinaryRequestTimeout        = 45 * time.Second
	RemoteBrowseRequestTimeout    = 75 * time.Second
	ConnectorActionRequestTimeout = 90 * time.Second
)

type Lifecycle interface {
	AcquireMutation() func()
	AcquireRead() func()
}

type Boundary struct {
	Routes            http.Handler
	Lifecycle         Lifecycle
	IsUnlocked        func() bool
	IsLocalRemoteAddr func(string) bool
	IsLocalhostHeader func(string) bool
	AllowsOrigin      func(string) bool
	HasSession        func(*http.Request) bool
	EnsureWorkspace   func(http.ResponseWriter, *http.Request)
	HasCSRF           func(*http.Request) bool
	IsSessionExempt   func(string) bool
	RequiresCSRF      func(string, string) bool
	WriteError        func(http.ResponseWriter, int, string)
}

func (boundary Boundary) Handler() http.Handler {
	handler := http.HandlerFunc(boundary.serveHTTP)
	return ResponsePolicy(boundary.localOnly(boundary.cors(WithRequestDeadline(handler, OrdinaryRequestTimeout))))
}

func ResponsePolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, private")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (boundary Boundary) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if boundary.IsLocalRemoteAddr == nil || !boundary.IsLocalRemoteAddr(r.RemoteAddr) {
			boundary.writeError(w, http.StatusForbidden, "remote gateway access is disabled; connect from localhost")
			return
		}
		if boundary.IsLocalhostHeader == nil || !boundary.IsLocalhostHeader(r.Host) {
			boundary.writeError(w, http.StatusForbidden, "remote gateway host header is disabled; use localhost")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (boundary Boundary) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if !boundary.allowedOrigin(origin) {
				boundary.writeError(w, http.StatusForbidden, "origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-AIPermission-CSRF")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (boundary Boundary) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if IsStateChangingMethod(r.Method) && !boundary.safeBrowserMutationSource(r) {
		boundary.writeError(w, http.StatusForbidden, "cross-site mutation requests are not allowed")
		return
	}
	streaming, managesLifecycle := IsStreamingRoute(r.URL.Path), ManagesLifecycleLock(r.URL.Path)
	if !streaming && !managesLifecycle {
		var release func()
		if IsLifecycleMutation(r.URL.Path) {
			release = boundary.Lifecycle.AcquireMutation()
		} else {
			release = boundary.Lifecycle.AcquireRead()
		}
		defer release()
	}
	unlocked := boundary.IsUnlocked != nil && boundary.IsUnlocked()
	if !unlocked && !isAllowedWhileLocked(r.URL.Path) {
		boundary.writeError(w, http.StatusLocked, "database is locked")
		return
	}
	if unlocked && (boundary.IsSessionExempt == nil || !boundary.IsSessionExempt(r.URL.Path)) {
		if boundary.HasSession == nil || !boundary.HasSession(r) {
			boundary.writeError(w, http.StatusUnauthorized, "ui session required")
			return
		}
		if boundary.EnsureWorkspace != nil {
			boundary.EnsureWorkspace(w, r)
		}
	}
	if unlocked && boundary.RequiresCSRF != nil && boundary.RequiresCSRF(r.Method, r.URL.Path) &&
		(boundary.HasCSRF == nil || !boundary.HasCSRF(r)) {
		boundary.writeError(w, http.StatusForbidden, "csrf token required")
		return
	}
	boundary.Routes.ServeHTTP(w, r)
}

func (boundary Boundary) safeBrowserMutationSource(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return boundary.allowedOrigin(origin) && safeFetchSite(r.Header.Get("Sec-Fetch-Site"))
	}
	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer != "" {
		parsed, err := url.Parse(referer)
		return err == nil && parsed.Scheme != "" && parsed.Host != "" &&
			boundary.allowedOrigin(parsed.Scheme+"://"+parsed.Host) && safeFetchSite(r.Header.Get("Sec-Fetch-Site"))
	}
	return !looksLikeBrowserMutation(r) && safeFetchSite(r.Header.Get("Sec-Fetch-Site"))
}

func (boundary Boundary) allowedOrigin(origin string) bool {
	return origin == "" || boundary.AllowsOrigin != nil && boundary.AllowsOrigin(origin)
}

func (boundary Boundary) writeError(w http.ResponseWriter, status int, message string) {
	if boundary.WriteError != nil {
		boundary.WriteError(w, status, message)
		return
	}
	http.Error(w, message, status)
}

func safeFetchSite(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "same-origin", "same-site", "none":
		return true
	default:
		return false
	}
}

func looksLikeBrowserMutation(r *http.Request) bool {
	ua, accept, mode := strings.ToLower(r.Header.Get("User-Agent")), strings.ToLower(r.Header.Get("Accept")), strings.ToLower(r.Header.Get("Sec-Fetch-Mode"))
	return strings.Contains(ua, "mozilla/") || strings.Contains(accept, "text/html") || mode == "navigate" || mode == "no-cors"
}

func IsStateChangingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func IsLifecycleMutation(path string) bool {
	switch path {
	case "/api/unlock/setup", "/api/unlock", "/api/lock", "/api/databases/rename", "/api/databases/delete",
		"/api/databases/delete-locked", "/api/databases/switch", "/api/databases/change-password", "/api/backup/import", "/api/backup/remote/restore":
		return true
	default:
		return false
	}
}

func IsStreamingRoute(path string) bool {
	return path == "/api/settings/maintenance-console/attach" || strings.HasPrefix(path, "/api/console/sessions/") && strings.HasSuffix(path, "/attach")
}

func IsUnboundedRequestRoute(path string) bool {
	if IsStreamingRoute(path) || path == "/api/backup/download" || path == "/api/backup/import" || path == "/api/backup/remote/restore" {
		return true
	}
	if strings.HasPrefix(path, "/api/backup/providers/") && (strings.HasSuffix(path, "/upload") || strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/restore")) {
		return true
	}
	if strings.HasPrefix(path, "/api/file-transfers/") && strings.HasSuffix(path, "/download") || strings.HasPrefix(path, "/api/file-transfer-batches/") && strings.HasSuffix(path, "/download") {
		return true
	}
	if path == "/api/file-transfers/upload" || path == "/api/file-transfers/upload-batch" {
		return true
	}
	return strings.HasPrefix(path, "/api/connector-targets/") && (strings.HasSuffix(path, "/backup") || strings.HasSuffix(path, "/restore"))
}

func ManagesLifecycleLock(path string) bool { return path == "/api/backup/download" }

func WithRequestDeadline(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if timeout <= 0 || IsUnboundedRequestRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		deadline := time.Now().Add(RequestTimeoutForPath(r.URL.Path, timeout))
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(deadline)
		_ = controller.SetWriteDeadline(deadline)
		defer func() { _ = controller.SetReadDeadline(time.Time{}); _ = controller.SetWriteDeadline(time.Time{}) }()
		ctx, cancel := context.WithDeadline(r.Context(), deadline)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestTimeoutForPath(path string, fallback time.Duration) time.Duration {
	switch path {
	case "/api/file-transfers/browse", "/api/file-transfers/expand":
		return RemoteBrowseRequestTimeout
	case "/api/connector-actions/local-run", "/api/mcp/connector-actions/call":
		return ConnectorActionRequestTimeout
	default:
		return fallback
	}
}

func isAllowedWhileLocked(path string) bool {
	return path == "/health" || path == "/api/status" || path == "/api/unlock/status" || path == "/api/unlock/setup" || path == "/api/unlock" ||
		path == "/api/backup/import" || path == "/api/backup/remote/list" || path == "/api/backup/remote/restore" || path == "/api/databases/delete-locked"
}
