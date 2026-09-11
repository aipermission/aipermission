package api

import (
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

const (
	uiSessionCookieName   = gatewayaccess.SessionCookieBase
	uiCSRFCookieName      = gatewayaccess.CSRFCookieBase
	uiWorkspaceCookieName = gatewayaccess.WorkspaceCookieBase
	uiCSRFHeaderName      = gatewayaccess.CSRFHeaderName
	uiSessionMaxAge       = gatewayaccess.SessionMaxAge
)

type preparedUISession = gatewayaccess.PreparedUISession

// issueUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issueUISessionLocked(w http.ResponseWriter) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.access.IssueUISession(w, databaseID, retryIdentity)
}

// issuePreparedUISessionLocked requires s.mu to be held by the lifecycle caller.
func (s *Server) issuePreparedUISessionLocked(w http.ResponseWriter, prepared preparedUISession) error {
	databaseID, retryIdentity := s.activeUIWorkspaceLocked()
	return s.access.IssuePreparedUISession(w, prepared, databaseID, retryIdentity)
}

func (s *Server) clearUISessions(w http.ResponseWriter) { s.access.ClearUISessions(w) }

func (s *Server) hasValidUISession(r *http.Request) bool {
	return s.access.ValidUISession(r, s.workspaceSelection().ID)
}

func (s *Server) hasValidUICSRF(r *http.Request) bool { return s.access.ValidUICSRF(r) }

func (s *Server) ensureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request) {
	_, retryIdentity := s.activeUIWorkspaceLocked()
	s.access.EnsureUIWorkspaceCookie(w, r, retryIdentity)
}

func (s *Server) activeUIWorkspaceLocked() (string, string) {
	databaseID := s.workspaceSelection().ID
	if runtime, ok := s.infrastructure.LookupWorkspace(databaseID); ok && runtime != nil {
		return databaseID, runtime.Identity.UIRetryID
	}
	return databaseID, ""
}
