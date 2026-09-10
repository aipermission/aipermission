package api

import (
	"context"
	"database/sql"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func localExecutionPrincipal(runtime *databaseRuntime) (gatewayaccess.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return gatewayaccess.PrincipalLocalOperator(runtime.WorkspaceUUID, runtime.RuntimeInstanceID)
}

func tokenExecutionPrincipal(runtime *databaseRuntime, tokenID int64) (gatewayaccess.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return gatewayaccess.Principal{}, err
	}
	return gatewayaccess.PrincipalMCPToken(tokenID, runtime.WorkspaceUUID, runtime.RuntimeInstanceID)
}

func ensureRuntimeIdentity(runtime *databaseRuntime) error {
	if runtime == nil {
		return gatewayaccess.ErrInvalidPrincipal
	}
	return runtime.EnsureIdentity(func(database *sql.DB) (string, error) {
		return gatewayvault.EnsureWorkspaceUUID(context.Background(), database)
	}, gatewayaccess.NewRuntimeInstanceID)
}
