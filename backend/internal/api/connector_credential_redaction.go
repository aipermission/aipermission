package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type connectorCredentialBoundary = actionresult.CredentialBoundary

func connectorCredentialBoundaryForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (actionresult.CredentialBoundary, error) {
	if runtime == nil || runtime.database == nil || runtime.vault == nil {
		return actionresult.CredentialBoundary{}, nil
	}
	store := connectortargets.NewStore(runtime.database)
	_, profileView, _, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, profileView.TargetID, profileView.ID)
	if err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	if profile.EncryptedSecretJSON == "" {
		return actionresult.CredentialBoundary{}, nil
	}
	secrets := map[string]any{}
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		return actionresult.CredentialBoundary{}, err
	}
	return actionresult.NewCredentialBoundary(secrets), nil
}
