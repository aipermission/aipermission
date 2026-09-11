package api

import (
	"context"
	"database/sql"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) localExecutionPrincipal(runtime databaseRuntime) (gatewayaccess.Principal, error) {
	if err := s.ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return s.access.PrincipalLocalOperator(runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}

func (s *Server) tokenExecutionPrincipal(runtime databaseRuntime, tokenID int64) (gatewayaccess.Principal, error) {
	if err := s.ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return s.access.PrincipalMCPToken(tokenID, runtime.WorkspaceIdentifier(), runtime.RuntimeIdentifier())
}

func (s *Server) ensureRuntimeIdentity(runtime databaseRuntime) error {
	if runtime == nil {
		return gatewayaccess.ErrInvalidPrincipal
	}
	return runtime.EnsureIdentity(func(database *sql.DB) (string, error) {
		return gatewayvault.EnsureWorkspaceUUID(context.Background(), database)
	}, s.access.NewRuntimeInstanceID)
}
