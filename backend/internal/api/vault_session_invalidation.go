package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func (s *Server) vaultSessionInvalidator(runtime *databaseRuntime) (*vaultsessions.Invalidator, error) {
	if s == nil || runtime == nil || runtime.database == nil ||
		runtime.vaultLeases == nil || runtime.consoleSessions == nil {
		return nil, vaultsessions.ErrInvalidatorUnavailable
	}
	return vaultsessions.NewInvalidator(vaultsessions.InvalidatorDependencies{
		Persistence: vaultsessions.NewPersistence(runtime.database),
		Leases:      runtime.vaultLeases,
		Sessions:    runtime.consoleSessions,
		Principal: func() (executionprincipal.Principal, error) {
			return localExecutionPrincipal(runtime)
		},
		Requests: func(ctx context.Context) (vaultsessions.RequestInvalidator, error) {
			owner, err := s.vaultRequestRuntime(ctx, runtime)
			if err != nil {
				return nil, err
			}
			return owner, nil
		},
	})
}

func (s *Server) invalidateVaultMutationAfterCommit(
	ctx context.Context,
	runtime *databaseRuntime,
	sessions []projectvault.SessionReference,
	scope projectvault.SessionMutationScope,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	references := make([]vaultsessions.Reference, len(sessions))
	for index, session := range sessions {
		references[index] = vaultsessions.Reference{
			SessionID: session.SessionID, RuntimeID: session.RuntimeID, Generation: session.Generation,
		}
	}
	return owner.InvalidateMutation(
		ctx, references, scope.ItemID, scope.BindingID,
		"Vault item or binding changed; send a fresh request",
	)
}

func (s *Server) finishVaultTokenSessionInvalidation(
	ctx context.Context,
	runtime *databaseRuntime,
	tokenID int64,
	sessionIDs []int64,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	return owner.FinishTokenInvalidation(ctx, tokenID, sessionIDs)
}

func (s *Server) invalidateVaultProjectSessions(
	ctx context.Context,
	runtime *databaseRuntime,
	projectID int64,
	reason string,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	return owner.InvalidateProject(ctx, projectID, reason)
}

func (s *Server) invalidateVaultRuntimeSessions(
	ctx context.Context,
	runtime *databaseRuntime,
	runtimeIDs []int64,
	reason string,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	return owner.InvalidateRuntimes(ctx, runtimeIDs, reason)
}

func (s *Server) invalidateVaultSessionsForTargetProfile(
	ctx context.Context,
	runtime *databaseRuntime,
	targetID int64,
	profileID int64,
	reason string,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	return owner.InvalidateTargetProfile(ctx, targetID, profileID, reason)
}

func (s *Server) invalidateAllVaultSessions(
	ctx context.Context,
	runtime *databaseRuntime,
	reason string,
) error {
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	return owner.InvalidateAll(ctx, reason)
}
