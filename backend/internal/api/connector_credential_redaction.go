package api

import (
	"context"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type connectorCredentialBoundary = actions.CredentialBoundary

func connectorCredentialBoundaryForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (actions.CredentialBoundary, error) {
	if runtime == nil || runtime.Storage.Database == nil || runtime.Storage.Vault == nil {
		return actions.CredentialBoundary{}, nil
	}
	store := connectormgmt.NewStore(runtime.Storage.Database)
	_, profileView, _, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return actions.CredentialBoundary{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, profileView.TargetID, profileView.ID)
	if err != nil {
		return actions.CredentialBoundary{}, err
	}
	if profile.EncryptedSecretJSON == "" {
		return actions.CredentialBoundary{}, nil
	}
	secrets := map[string]any{}
	if err := gatewayvault.DecryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, gatewayvault.ConnectorCredentialProfileRecord(), profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		return actions.CredentialBoundary{}, err
	}
	return actions.NewCredentialBoundary(secrets), nil
}
