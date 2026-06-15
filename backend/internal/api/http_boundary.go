package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) Handler() http.Handler {
	return s.withCORS(http.HandlerFunc(s.serveHTTP))
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
	if unlocked && requiresUICSRF(r.Method, r.URL.Path) && !hasValidUICSRF(r) {
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
		"/api/backup/import":
		return true
	default:
		return false
	}
}

func isStreamingRoute(path string) bool {
	return strings.HasPrefix(path, "/api/console/sessions/") && strings.HasSuffix(path, "/attach")
}

func managesLifecycleLock(path string) bool {
	return path == "/api/backup/download"
}

func isLocalhostHeader(hostHeader string) bool {
	hostHeader = strings.TrimSpace(hostHeader)
	if hostHeader == "" {
		return true
	}
	host, _, err := net.SplitHostPort(hostHeader)
	if err != nil {
		host = hostHeader
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func isLocalRemoteAddr(remoteAddr string) bool {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return false
}
