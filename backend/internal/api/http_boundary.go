package api

import (
	"log"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/gatewayhttp"
)

func (s *Server) Handler() http.Handler {
	return gatewayhttp.Boundary{
		Routes: s.mux, Lifecycle: s.workspaceState.Lifecycle, IsUnlocked: s.isUnlocked,
		IsLocalRemoteAddr: s.config.IsLocalRemoteAddr, IsLocalhostHeader: s.config.IsLocalhostHeader,
		AllowsOrigin: s.config.AllowsOrigin, HasSession: s.hasValidUISession,
		EnsureWorkspace: s.ensureUIWorkspaceCookie, HasCSRF: s.hasValidUICSRF,
		IsSessionExempt: isUISessionExempt, RequiresCSRF: requiresUICSRF,
		WriteError: writeError,
	}.Handler()
}

func (s *Server) Close() {
	release := s.workspaceState.Lifecycle.AcquireMutation()
	defer release()
	s.closeMaintenanceConsoleForLifecycle("server_shutdown")
	if err := s.workspaceState.Lifecycle.CloseAll(); err != nil {
		log.Printf("close unlocked database resources failed: %v", err)
	}
}
