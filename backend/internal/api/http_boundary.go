package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api/httptransport"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
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
	release, err := s.workspaceOwner.WorkspaceLifecycle().AcquireMutationContext(ctx)
	if err != nil {
		return err
	}
	defer release()
	s.closeMaintenanceConsoleForLifecycle("server_shutdown")
	runtimes := s.workspaceOwner.OwnedWorkspaceSnapshot()
	closeErr := s.workspaceOwner.WorkspaceLifecycle().CloseAll(ctx)
	waits := make(chan error, len(runtimes))
	for _, runtime := range runtimes {
		go func(runtime *gatewayinfra.WorkspaceHandle) {
			waits <- s.workspaceOwner.WaitWorkspaceClosed(ctx, runtime)
		}(runtime)
	}
	var waitErrors []error
	for range runtimes {
		select {
		case waitErr := <-waits:
			if waitErr != nil {
				waitErrors = append(waitErrors, waitErr)
			}
		case <-ctx.Done():
			waitErrors = append(waitErrors, ctx.Err())
			return errors.Join(closeErr, errors.Join(waitErrors...))
		}
	}
	waitErr := errors.Join(waitErrors...)
	return errors.Join(closeErr, waitErr)
}
