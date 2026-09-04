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
	uiSessionCookieName   = "aipermission_ui_session"
	uiCSRFCookieName      = "aipermission_csrf"
	uiWorkspaceCookieName = "aipermission_workspace"
	uiCSRFHeaderName      = "X-AIPermission-CSRF"
	uiSessionMaxAge       = 12 * time.Hour
)

type uiSessionRecord struct {
	Expires    time.Time
	DatabaseID string
}

type preparedUISession struct {
	token   string
	csrf    string
	hash    string
	expires time.Time
}

func prepareUISession() (preparedUISession, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return preparedUISession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expires := time.Now().UTC().Add(uiSessionMaxAge)
	csrfBytes := make([]byte, 32)
	if _, err := rand.Read(csrfBytes); err != nil {
		return preparedUISession{}, err
	}
	return preparedUISession{
		token:   token,
		csrf:    base64.RawURLEncoding.EncodeToString(csrfBytes),
		hash:    hashUISessionToken(token),
		expires: expires,
	}, nil
}

// issueUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issueUISessionLocked(w http.ResponseWriter) error {
	prepared, err := prepareUISession()
	if err != nil {
		return err
	}
	s.issuePreparedUISessionLocked(w, prepared)
	return nil
}

// issuePreparedUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issuePreparedUISessionLocked(w http.ResponseWriter, prepared preparedUISession) {
	databaseID := s.activeDatabase
	workspaceID := ""
	if runtime := s.workspaces[databaseID]; runtime != nil {
		workspaceID = runtime.workspaceUUID
	}
	retryIdentity := uiRetryIdentity(databaseID, workspaceID)

	s.uiSessionMu.Lock()
	if s.uiSessions == nil {
		s.uiSessions = map[string]uiSessionRecord{}
	}
	s.pruneUISessionsLocked(time.Now().UTC())
	s.uiSessions[prepared.hash] = uiSessionRecord{Expires: prepared.expires, DatabaseID: databaseID}
	s.uiSessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.uiSessionCookieName(),
		Value:    prepared.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  prepared.expires,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.uiCSRFCookieName(),
		Value:    prepared.csrf,
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  prepared.expires,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.uiWorkspaceCookieName(),
		Value:    retryIdentity,
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  prepared.expires,
	})
}

func (s *Server) clearUISessions(w http.ResponseWriter) {
	s.uiSessionMu.Lock()
	s.uiSessions = map[string]uiSessionRecord{}
	s.uiSessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.uiSessionCookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.uiCSRFCookieName(),
		Value:    "",
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.uiWorkspaceCookieName(),
		Value:    "",
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
}

func (s *Server) hasValidUISession(r *http.Request) bool {
	cookie, err := r.Cookie(s.uiSessionCookieName())
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

func (s *Server) hasValidUICSRF(r *http.Request) bool {
	cookie, err := r.Cookie(s.uiCSRFCookieName())
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(uiCSRFHeaderName))
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

func (s *Server) ensureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	databaseID := s.activeDatabase
	runtime := s.workspaces[databaseID]
	s.mu.RUnlock()
	if runtime == nil || strings.TrimSpace(runtime.workspaceUUID) == "" {
		return
	}
	retryIdentity := uiRetryIdentity(databaseID, runtime.workspaceUUID)
	if cookie, err := r.Cookie(s.uiWorkspaceCookieName()); err == nil && strings.TrimSpace(cookie.Value) == retryIdentity {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.uiWorkspaceCookieName(),
		Value:    retryIdentity,
		Path:     "/",
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(uiSessionMaxAge.Seconds()),
		Expires:  time.Now().UTC().Add(uiSessionMaxAge),
	})
}

func uiRetryIdentity(databaseID, workspaceID string) string {
	if strings.TrimSpace(databaseID) == "" || strings.TrimSpace(workspaceID) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(databaseID + "\x00" + workspaceID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
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

func (s *Server) uiSessionCookieName() string {
	return scopedUICookieName(uiSessionCookieName, s.config.FrontendPort)
}

func (s *Server) uiCSRFCookieName() string {
	return scopedUICookieName(uiCSRFCookieName, s.config.FrontendPort)
}

func (s *Server) uiWorkspaceCookieName() string {
	return scopedUICookieName(uiWorkspaceCookieName, s.config.FrontendPort)
}

func scopedUICookieName(base string, frontendPort string) string {
	frontendPort = strings.TrimSpace(frontendPort)
	if frontendPort == "" {
		return base
	}
	var scope strings.Builder
	for _, char := range frontendPort {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '-', char == '_':
			scope.WriteRune(char)
		default:
			scope.WriteByte('_')
		}
	}
	if scope.Len() == 0 {
		return base
	}
	return base + "_" + scope.String()
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
