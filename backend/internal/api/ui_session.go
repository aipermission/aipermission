package api

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/uisession"
)

const (
	uiSessionCookieName   = uisession.SessionCookieBase
	uiCSRFCookieName      = uisession.CSRFCookieBase
	uiWorkspaceCookieName = uisession.WorkspaceCookieBase
	uiCSRFHeaderName      = uisession.CSRFHeaderName
	uiSessionMaxAge       = uisession.SessionMaxAge
)

type preparedUISession = uisession.Prepared

func prepareUISession() (preparedUISession, error) { return uisession.Prepare() }

// issueUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issueUISessionLocked(w http.ResponseWriter) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.uiSessions.Issue(w, databaseID, retryIdentity)
}

// issuePreparedUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issuePreparedUISessionLocked(w http.ResponseWriter, prepared preparedUISession) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.uiSessions.IssuePrepared(w, prepared, databaseID, retryIdentity)
}

func (s *Server) clearUISessions(w http.ResponseWriter) { s.uiSessions.Clear(w) }

func (s *Server) hasValidUISession(r *http.Request) bool {
	s.mu.RLock()
	activeDatabase := s.activeDatabase
	s.mu.RUnlock()
	return s.uiSessions.Valid(r, activeDatabase)
}

func (s *Server) hasValidUICSRF(r *http.Request) bool { return s.uiSessions.ValidCSRF(r) }

func (s *Server) ensureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	_, retryIdentity := s.activeUIWorkspaceLocked()
	s.mu.RUnlock()
	s.uiSessions.EnsureWorkspaceCookie(w, r, retryIdentity)
}

func (s *Server) activeUIWorkspaceLocked() (string, string) {
	databaseID := s.activeDatabase
	if runtime := s.workspaces[databaseID]; runtime != nil {
		return databaseID, runtime.uiRetryIdentity
	}
	return databaseID, ""
}

func uiRetryIdentity(instanceID string) string { return uisession.RetryIdentity(instanceID) }

func isUISessionExempt(path string) bool { return uisession.IsExempt(path) }

func requiresUICSRF(method, path string) bool {
	return !uisession.IsExempt(path) && isStateChangingMethod(method)
}
