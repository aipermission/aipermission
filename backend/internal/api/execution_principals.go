package api

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func localExecutionPrincipal(runtime *databaseRuntime) (executionprincipal.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.LocalOperator(runtime.WorkspaceUUID, runtime.RuntimeInstanceID)
}

func tokenExecutionPrincipal(runtime *databaseRuntime, tokenID int64) (executionprincipal.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.MCPToken(tokenID, runtime.WorkspaceUUID, runtime.RuntimeInstanceID)
}

func ensureRuntimeIdentity(runtime *databaseRuntime) error {
	if runtime == nil {
		return executionprincipal.ErrInvalid
	}
	return runtime.EnsureIdentity(func(database *sql.DB) (string, error) {
		return projectvault.EnsureWorkspaceUUID(context.Background(), database)
	}, executionprincipal.NewRuntimeInstanceID)
}
