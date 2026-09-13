package identity

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type State struct {
	GatewaySecret     string
	WorkspaceUUID     string
	UIRetryIdentity   string
	RuntimeInstanceID string
	ActionIdentityKey []byte
	Vault             *vault.Vault
}

func Initialize(ctx context.Context, database *sql.DB, configuredGatewaySecret string) (State, error) {
	bindingRequired, err := recordcrypto.EnvelopeMarkerPresent(ctx, database)
	if err != nil {
		return State{}, err
	}
	gatewaySecret, err := projectvault.ResolveGatewaySecret(ctx, database, configuredGatewaySecret)
	if err != nil {
		return State{}, err
	}
	secretVault, err := vault.New(gatewaySecret)
	if err != nil {
		return State{}, err
	}
	workspaceUUID, err := workspaceUUID(ctx, database, bindingRequired)
	if err != nil {
		return State{}, err
	}
	uiRetryIdentity, err := projectvault.EnsureUIRetryIdentity(ctx, database)
	if err != nil {
		return State{}, err
	}
	if _, err := recordcrypto.RewriteLegacy(ctx, database, secretVault, workspaceUUID); err != nil {
		return State{}, fmt.Errorf("migrate encrypted records: %w", err)
	}
	actionIdentityKey, err := actions.DeriveIdentityKey(gatewaySecret, workspaceUUID)
	if err != nil {
		return State{}, err
	}
	runtimeInstanceID, err := executionprincipal.NewRuntimeInstanceID()
	if err != nil {
		actions.ClearIdentityKey(actionIdentityKey)
		return State{}, err
	}
	return State{
		GatewaySecret: gatewaySecret, WorkspaceUUID: workspaceUUID,
		UIRetryIdentity: uiRetryIdentity, RuntimeInstanceID: runtimeInstanceID,
		ActionIdentityKey: actionIdentityKey, Vault: secretVault,
	}, nil
}

func workspaceUUID(ctx context.Context, database *sql.DB, bindingRequired bool) (string, error) {
	if bindingRequired {
		return projectvault.ReadWorkspaceUUID(ctx, database)
	}
	return projectvault.EnsureWorkspaceUUID(ctx, database)
}
