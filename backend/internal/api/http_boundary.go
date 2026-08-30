package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *Server) Handler() http.Handler {
	return s.withCORS(withRequestDeadline(http.HandlerFunc(s.serveHTTP), ordinaryRequestTimeout))
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeAllUnlockedResources()
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLocalRemoteAddr(r.RemoteAddr) {
		writeError(w, http.StatusForbidden, "remote gateway access is disabled; connect from localhost")
		return
	}
	if !isLocalhostHeader(r.Host) {
		writeError(w, http.StatusForbidden, "remote gateway host header is disabled; use localhost")
		return
	}
	if r.Method == http.MethodOptions {
		s.mux.ServeHTTP(w, r)
		return
	}
	if isStateChangingMethod(r.Method) && !s.hasSafeBrowserMutationSource(r) {
		writeError(w, http.StatusForbidden, "cross-site mutation requests are not allowed")
		return
	}

	unlocked := s.isUnlocked()
	if !unlocked && !isAllowedWhileLocked(r.URL.Path) {
		writeError(w, http.StatusLocked, "database is locked")
		return
	}
	if unlocked && !isUISessionExempt(r.URL.Path) && !s.hasValidUISession(r) {
		writeError(w, http.StatusUnauthorized, "ui session required")
		return
	}
	if unlocked && requiresUICSRF(r.Method, r.URL.Path) && !s.hasValidUICSRF(r) {
		writeError(w, http.StatusForbidden, "csrf token required")
		return
	}
	if isStreamingRoute(r.URL.Path) {
		s.mux.ServeHTTP(w, r)
		return
	}
	if managesLifecycleLock(r.URL.Path) {
		s.mux.ServeHTTP(w, r)
		return
	}
	if isLifecycleMutation(r.URL.Path) {
		s.lifecycleMu.Lock()
		defer s.lifecycleMu.Unlock()
	} else {
		s.lifecycleMu.RLock()
		defer s.lifecycleMu.RUnlock()
	}
	s.mux.ServeHTTP(w, r)
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) hasSafeBrowserMutationSource(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return s.isAllowedOrigin(origin) && isSafeFetchSite(r.Header.Get("Sec-Fetch-Site"))
	}
	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer != "" {
		return s.isAllowedReferer(referer) && isSafeFetchSite(r.Header.Get("Sec-Fetch-Site"))
	}
	if looksLikeBrowserMutation(r) {
		return false
	}
	return isSafeFetchSite(r.Header.Get("Sec-Fetch-Site"))
}

func isSafeFetchSite(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "same-origin", "same-site", "none":
		return true
	default:
		return false
	}
}

func (s *Server) isAllowedReferer(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return s.isAllowedOrigin(parsed.Scheme + "://" + parsed.Host)
}

func looksLikeBrowserMutation(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	accept := strings.ToLower(r.Header.Get("Accept"))
	mode := strings.ToLower(r.Header.Get("Sec-Fetch-Mode"))
	return strings.Contains(ua, "mozilla/") || strings.Contains(accept, "text/html") || mode == "navigate" || mode == "no-cors"
}

func isLifecycleMutation(path string) bool {
	switch path {
	case "/api/unlock/setup", "/api/unlock", "/api/lock",
		"/api/databases/rename", "/api/databases/delete", "/api/databases/delete-locked", "/api/databases/switch", "/api/databases/change-password",
		"/api/backup/import", "/api/backup/remote/restore":
		return true
	default:
		return false
	}
}

func isStreamingRoute(path string) bool {
	return path == "/api/settings/maintenance-console/attach" ||
		(strings.HasPrefix(path, "/api/console/sessions/") && strings.HasSuffix(path, "/attach"))
}

func isUnboundedRequestRoute(path string) bool {
	if isStreamingRoute(path) {
		return true
	}
	if path == "/api/backup/download" || path == "/api/backup/import" || path == "/api/backup/remote/restore" {
		return true
	}
	if strings.HasPrefix(path, "/api/backup/providers/") &&
		(strings.HasSuffix(path, "/upload") || strings.HasSuffix(path, "/download") || strings.HasSuffix(path, "/restore")) {
		return true
	}
	if strings.HasPrefix(path, "/api/file-transfers/") && strings.HasSuffix(path, "/download") {
		return true
	}
	if strings.HasPrefix(path, "/api/file-transfer-batches/") && strings.HasSuffix(path, "/download") {
		return true
	}
	if path == "/api/file-transfers/upload" || path == "/api/file-transfers/upload-batch" {
		return true
	}
	return strings.HasPrefix(path, "/api/connector-targets/") &&
		(strings.HasSuffix(path, "/backup") || strings.HasSuffix(path, "/restore"))
}

func managesLifecycleLock(path string) bool {
	return path == "/api/backup/download"
}

func isLocalhostHeader(hostHeader string) bool {
	hostHeader = strings.TrimSpace(hostHeader)
	if hostHeader == "" {
		return false
	}
	host, ok := parseHostHeader(hostHeader)
	if !ok {
		return false
	}
	return isLoopbackHost(host)
}

func isLocalRemoteAddr(remoteAddr string) bool {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return false
	}
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil || !validTCPPort(port) {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseHostHeader(value string) (string, bool) {
	if strings.HasPrefix(value, "[") {
		if strings.HasSuffix(value, "]") {
			host := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			return host, host != ""
		}
		host, port, err := net.SplitHostPort(value)
		return host, err == nil && host != "" && validTCPPort(port)
	}
	if strings.Count(value, ":") == 0 {
		return value, true
	}
	host, port, err := net.SplitHostPort(value)
	return host, err == nil && host != "" && validTCPPort(port)
}

func validTCPPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
