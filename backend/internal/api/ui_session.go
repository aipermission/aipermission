package api

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

const (
	uiSessionCookieName   = gatewayaccess.SessionCookieBase
	uiCSRFCookieName      = gatewayaccess.CSRFCookieBase
	uiWorkspaceCookieName = gatewayaccess.WorkspaceCookieBase
	uiCSRFHeaderName      = gatewayaccess.CSRFHeaderName
	uiSessionMaxAge       = gatewayaccess.SessionMaxAge
)

type preparedUISession = gatewayaccess.PreparedUISession

func prepareUISession() (preparedUISession, error) { return gatewayaccess.PrepareUISession() }

// issueUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issueUISessionLocked(w http.ResponseWriter) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.controlState.UISessions.Issue(w, databaseID, retryIdentity)
}

// issuePreparedUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issuePreparedUISessionLocked(w http.ResponseWriter, prepared preparedUISession) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.controlState.UISessions.IssuePrepared(w, prepared, databaseID, retryIdentity)
}

func (s *Server) clearUISessions(w http.ResponseWriter) { s.controlState.UISessions.Clear(w) }

func (s *Server) hasValidUISession(r *http.Request) bool {
	return s.controlState.UISessions.Valid(r, s.workspaceSelection().ID)
}

func (s *Server) hasValidUICSRF(r *http.Request) bool { return s.controlState.UISessions.ValidCSRF(r) }

func (s *Server) ensureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request) {
	_, retryIdentity := s.activeUIWorkspaceLocked()
	s.controlState.UISessions.EnsureWorkspaceCookie(w, r, retryIdentity)
}

func (s *Server) activeUIWorkspaceLocked() (string, string) {
	databaseID := s.workspaceSelection().ID
	if runtime, ok := s.workspaceState.Registry.Lookup(databaseID); ok && runtime != nil {
		return databaseID, runtime.UIRetryIdentifier()
	}
	return databaseID, ""
}

func uiRetryIdentity(instanceID string) string {
	return gatewayaccess.UISessionRetryIdentity(instanceID)
}

func isUISessionExempt(path string) bool { return gatewayaccess.IsUIExempt(path) }

func requiresUICSRF(method, path string) bool {
	return !gatewayaccess.IsUIExempt(path) && gatewayoperations.IsStateChangingMethod(method)
}
