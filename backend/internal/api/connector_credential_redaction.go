package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type connectorCredentialBoundary = actions.CredentialBoundary

func connectorCredentialBoundaryForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (actions.CredentialBoundary, error) {
	if runtime == nil || runtime.Storage.Database == nil || runtime.Storage.Vault == nil {
		return actions.CredentialBoundary{}, nil
	}
	store := connectortargets.NewStore(runtime.Storage.Database)
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
	if err := recordcrypto.DecryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		return actions.CredentialBoundary{}, err
	}
	return actions.NewCredentialBoundary(secrets), nil
}
