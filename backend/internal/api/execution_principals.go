package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

func localExecutionPrincipal(runtime *databaseRuntime) (executionprincipal.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.LocalOperator(runtime.workspaceUUID, runtime.runtimeInstanceID)
}

func tokenExecutionPrincipal(runtime *databaseRuntime, tokenID int64) (executionprincipal.Principal, error) {
	if err := ensureRuntimeIdentity(runtime); err != nil {
		return executionprincipal.Principal{}, err
	}
	return executionprincipal.MCPToken(tokenID, runtime.workspaceUUID, runtime.runtimeInstanceID)
}

func ensureRuntimeIdentity(runtime *databaseRuntime) error {
	if runtime == nil {
		return executionprincipal.ErrInvalid
	}
	runtime.identityMu.Lock()
	defer runtime.identityMu.Unlock()
	var err error
	if runtime.workspaceUUID == "" {
		runtime.workspaceUUID, err = projectvault.EnsureWorkspaceUUID(context.Background(), runtime.database)
		if err != nil {
			return err
		}
	}
	if runtime.runtimeInstanceID == "" {
		runtime.runtimeInstanceID, err = executionprincipal.NewRuntimeInstanceID()
	}
	return err
}
