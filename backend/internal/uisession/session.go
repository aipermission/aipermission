package uisession

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnavailable     = errors.New("UI session manager is unavailable")
	ErrInvalidPrepared = errors.New("prepared UI session is invalid or expired")
)

const (
	SessionCookieBase   = "aipermission_ui_session"
	CSRFCookieBase      = "aipermission_csrf"
	WorkspaceCookieBase = "aipermission_workspace"
	CSRFHeaderName      = "X-AIPermission-CSRF"
	SessionMaxAge       = 12 * time.Hour
)

type sessionRecord struct {
	expires    time.Time
	databaseID string
}

type Prepared struct {
	token   string
	csrf    string
	hash    string
	expires time.Time
}

type Manager struct {
	mu              sync.RWMutex
	sessions        map[string]sessionRecord
	sessionCookie   string
	csrfCookie      string
	workspaceCookie string
	now             func() time.Time
}

func New(frontendPort string) *Manager {
	return &Manager{
		sessions:        map[string]sessionRecord{},
		sessionCookie:   scopedCookieName(SessionCookieBase, frontendPort),
		csrfCookie:      scopedCookieName(CSRFCookieBase, frontendPort),
		workspaceCookie: scopedCookieName(WorkspaceCookieBase, frontendPort),
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func Prepare() (Prepared, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Prepared{}, err
	}
	csrfBytes := make([]byte, 32)
	if _, err := rand.Read(csrfBytes); err != nil {
		return Prepared{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	return Prepared{
		token:   token,
		csrf:    base64.RawURLEncoding.EncodeToString(csrfBytes),
		hash:    hashToken(token),
		expires: time.Now().UTC().Add(SessionMaxAge),
	}, nil
}

func (m *Manager) Issue(w http.ResponseWriter, databaseID, retryInstanceID string) error {
	if m == nil || w == nil {
		return ErrUnavailable
	}
	prepared, err := Prepare()
	if err != nil {
		return err
	}
	return m.IssuePrepared(w, prepared, databaseID, retryInstanceID)
}

func (m *Manager) IssuePrepared(w http.ResponseWriter, prepared Prepared, databaseID, retryInstanceID string) error {
	if m == nil || w == nil {
		return ErrUnavailable
	}
	if prepared.token == "" || prepared.csrf == "" || prepared.hash == "" || !prepared.expires.After(m.currentTime()) {
		return ErrInvalidPrepared
	}
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = map[string]sessionRecord{}
	}
	m.pruneLocked(m.currentTime())
	m.sessions[prepared.hash] = sessionRecord{expires: prepared.expires, databaseID: databaseID}
	m.mu.Unlock()
	m.setCookies(w, prepared, RetryIdentity(retryInstanceID))
	return nil
}

func (m *Manager) Clear(w http.ResponseWriter) {
	if m == nil || w == nil {
		return
	}
	m.mu.Lock()
	m.sessions = map[string]sessionRecord{}
	m.mu.Unlock()
	expires := time.Unix(0, 0).UTC()
	for _, cookie := range []struct {
		name     string
		httpOnly bool
	}{{m.sessionCookie, true}, {m.csrfCookie, false}, {m.workspaceCookie, false}} {
		http.SetCookie(w, &http.Cookie{
			Name: cookie.name, Path: "/", HttpOnly: cookie.httpOnly, Secure: true,
			SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: expires,
		})
	}
}

func (m *Manager) Valid(r *http.Request, activeDatabase string) bool {
	if m == nil || r == nil {
		return false
	}
	cookie, err := r.Cookie(m.sessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	hash := hashToken(cookie.Value)
	now := m.currentTime()
	m.mu.RLock()
	session, ok := m.sessions[hash]
	m.mu.RUnlock()
	if !ok || session.databaseID != activeDatabase {
		return false
	}
	if !session.expires.After(now) {
		m.mu.Lock()
		delete(m.sessions, hash)
		m.mu.Unlock()
		return false
	}
	return true
}

func (m *Manager) InvalidateDatabase(databaseID string) {
	if m == nil || strings.TrimSpace(databaseID) == "" {
		return
	}
	m.mu.Lock()
	for hash, session := range m.sessions {
		if session.databaseID == databaseID {
			delete(m.sessions, hash)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) ValidCSRF(r *http.Request) bool {
	if m == nil || r == nil {
		return false
	}
	cookie, err := r.Cookie(m.csrfCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
	return header != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}

func (m *Manager) EnsureWorkspaceCookie(w http.ResponseWriter, r *http.Request, retryInstanceID string) {
	if m == nil || w == nil || r == nil || strings.TrimSpace(retryInstanceID) == "" {
		return
	}
	retryIdentity := RetryIdentity(retryInstanceID)
	if cookie, err := r.Cookie(m.workspaceCookie); err == nil && strings.TrimSpace(cookie.Value) == retryIdentity {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: m.workspaceCookie, Value: retryIdentity, Path: "/", Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(SessionMaxAge.Seconds()),
		Expires: m.currentTime().Add(SessionMaxAge),
	})
}

func RetryIdentity(instanceID string) string {
	if strings.TrimSpace(instanceID) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(instanceID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func IsExempt(path string) bool {
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

func (m *Manager) setCookies(w http.ResponseWriter, prepared Prepared, retryIdentity string) {
	maxAge := int(SessionMaxAge.Seconds())
	http.SetCookie(w, &http.Cookie{
		Name: m.sessionCookie, Value: prepared.token, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: prepared.expires,
	})
	http.SetCookie(w, &http.Cookie{
		Name: m.csrfCookie, Value: prepared.csrf, Path: "/", Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: prepared.expires,
	})
	http.SetCookie(w, &http.Cookie{
		Name: m.workspaceCookie, Value: retryIdentity, Path: "/", Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: prepared.expires,
	})
}

func (m *Manager) pruneLocked(now time.Time) {
	for hash, session := range m.sessions {
		if !session.expires.After(now) {
			delete(m.sessions, hash)
		}
	}
}

func (m *Manager) currentTime() time.Time {
	if m != nil && m.now != nil {
		return m.now().UTC()
	}
	return time.Now().UTC()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func scopedCookieName(base, frontendPort string) string {
	frontendPort = strings.TrimSpace(frontendPort)
	if frontendPort == "" {
		return base
	}
	var scope strings.Builder
	for _, char := range frontendPort {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9', char == '-', char == '_':
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
