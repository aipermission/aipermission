package api

import (
	"log"
	"net/http"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) Handler() http.Handler {
	return gatewayoperations.HTTPBoundary{
		Routes: s.mux, Lifecycle: s.infrastructure.WorkspaceLifecycle(), IsUnlocked: s.isUnlocked,
		IsLocalRemoteAddr: s.config.IsLocalRemoteAddr, IsLocalhostHeader: s.config.IsLocalhostHeader,
		AllowsOrigin: s.config.AllowsOrigin, HasSession: s.hasValidUISession,
		EnsureWorkspace: s.ensureUIWorkspaceCookie, HasCSRF: s.hasValidUICSRF,
		IsSessionExempt: s.access.IsUIExempt,
		RequiresCSRF: func(method, path string) bool {
			return !s.access.IsUIExempt(path) && gatewayoperations.IsStateChangingMethod(method)
		},
		WriteError: writeError,
	}.Handler()
}

func (s *Server) Close() {
	release := s.infrastructure.WorkspaceLifecycle().AcquireMutation()
	defer release()
	s.closeMaintenanceConsoleForLifecycle("server_shutdown")
	if err := s.infrastructure.WorkspaceLifecycle().CloseAll(); err != nil {
		log.Printf("close unlocked database resources failed: %v", err)
	}
}
