package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const (
	uiSessionCookieName = "aipermission_ui_session"
	uiCSRFCookieName    = "aipermission_csrf"
	uiCSRFHeaderName    = "X-AIPermission-CSRF"
	uiSessionMaxAge     = 12 * time.Hour
)

type uiSessionRecord struct {
	Expires    time.Time
	DatabaseID string
}

func (s *Server) issueUISession(w http.ResponseWriter) error {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expires := time.Now().UTC().Add(uiSessionMaxAge)
	hash := hashUISessionToken(token)
	databaseID := s.activeDatabase

	s.uiSessionMu.Lock()
	if s.uiSessions == nil {
		s.uiSessions = map[string]uiSessionRecord{}
	}
	s.pruneUISessionsLocked(time.Now().UTC())
	s.uiSessions[hash] = uiSessionRecord{Expires: expires, DatabaseID: databaseID}
	s.uiSessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  expires,
	})
	csrfBytes := make([]byte, 32)
	if _, err := rand.Read(csrfBytes); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiCSRFCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(csrfBytes),
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  expires,
	})
	return nil
}

func (s *Server) clearUISessions(w http.ResponseWriter) {
	s.uiSessionMu.Lock()
	s.uiSessions = map[string]uiSessionRecord{}
	s.uiSessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     uiCSRFCookieName,
		Value:    "",
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
}

func (s *Server) hasValidUISession(r *http.Request) bool {
	cookie, err := r.Cookie(uiSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	hash := hashUISessionToken(cookie.Value)
	now := time.Now().UTC()
	s.mu.RLock()
	activeDatabase := s.activeDatabase
	s.mu.RUnlock()

	s.uiSessionMu.RLock()
	session, ok := s.uiSessions[hash]
	s.uiSessionMu.RUnlock()
	if !ok {
		return false
	}
	if session.DatabaseID != activeDatabase {
		return false
	}
	if !session.Expires.After(now) {
		s.uiSessionMu.Lock()
		delete(s.uiSessions, hash)
		s.uiSessionMu.Unlock()
		return false
	}
	return true
}

func hasValidUICSRF(r *http.Request) bool {
	cookie, err := r.Cookie(uiCSRFCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(uiCSRFHeaderName))
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

func (s *Server) pruneUISessionsLocked(now time.Time) {
	for hash, session := range s.uiSessions {
		if !session.Expires.After(now) {
			delete(s.uiSessions, hash)
		}
	}
}

func hashUISessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func isUISessionExempt(path string) bool {
	if strings.HasPrefix(path, "/api/mcp/") {
		return true
	}
	switch path {
	case "/health", "/api/status", "/api/unlock/status", "/api/unlock":
		return true
	default:
		return false
	}
}

func requiresUICSRF(method string, path string) bool {
	if isUISessionExempt(path) {
		return false
	}
	return isStateChangingMethod(method)
}
