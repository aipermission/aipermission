package api

import (
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
)

func (s *Server) Handler() http.Handler {
	return httptransport.HTTPBoundary{
		Routes: s.mux, Lifecycle: s.workspaceOwner.WorkspaceLifecycle(), IsUnlocked: s.isUnlocked,
		IsLocalRemoteAddr: s.config.IsLocalRemoteAddr, IsLocalhostHeader: s.config.IsLocalhostHeader,
		AllowsOrigin: s.config.AllowsOrigin, HasSession: s.hasValidUISession,
		EnsureWorkspace: s.ensureUIWorkspaceCookie, HasCSRF: s.hasValidUICSRF,
		IsSessionExempt: s.access.IsUIExempt,
		RequiresCSRF: func(method, path string) bool {
			return !s.access.IsUIExempt(path) && httptransport.IsStateChangingMethod(method)
		},
		WriteError: writeError,
	}.Handler()
}

func (s *Server) Close() {
	release := s.workspaceOwner.WorkspaceLifecycle().AcquireMutation()
	defer release()
	s.closeMaintenanceConsoleForLifecycle("server_shutdown")
	if err := s.workspaceOwner.WorkspaceLifecycle().CloseAll(); err != nil {
		log.Printf("close unlocked database resources failed: %v", err)
	}
}
