package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
)

const defaultWorkspaceShutdownTimeout = 20 * time.Second

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
	ctx, cancel := context.WithTimeout(context.Background(), defaultWorkspaceShutdownTimeout)
	defer cancel()
	if err := s.CloseContext(ctx); err != nil {
		log.Printf("close unlocked database resources failed: %v", err)
	}
}

func (s *Server) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	release := s.workspaceOwner.WorkspaceLifecycle().AcquireMutation()
	defer release()
	s.closeMaintenanceConsoleForLifecycle("server_shutdown")
	runtimes := s.workspaceOwner.WorkspaceSnapshot()
	closeErr := s.workspaceOwner.WorkspaceLifecycle().CloseAll()
	var waitErrors []error
	for _, runtime := range runtimes {
		if err := s.workspaceOwner.WaitWorkspaceClosed(ctx, runtime); err != nil {
			waitErrors = append(waitErrors, err)
		}
	}
	waitErr := errors.Join(waitErrors...)
	return errors.Join(closeErr, waitErr)
}
