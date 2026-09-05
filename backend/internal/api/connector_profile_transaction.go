package api

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// The caller owns the transaction, audit events, and post-commit invalidation.
func (s connectorTargetHandlers) updatePreparedCredentialProfile(
	ctx context.Context,
	runtime *databaseRuntime,
	tx *sql.Tx,
	target connectortargets.Target,
	existing connectortargets.CredentialProfile,
	prepared preparedConnectorCredentialProfileInput,
) (connectortargets.CredentialProfile, error) {
	encrypted, err := encryptPreparedCredentialSecret(runtime, existing.ID, prepared)
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	store := connectortargets.NewTxStore(tx)
	profile, err := store.UpdateCredentialProfile(ctx, connectortargets.UpdateCredentialProfileInput{
		TargetID: target.ID, ProfileID: existing.ID, ConnectorKind: target.ConnectorKind,
		Kind: prepared.Kind, Label: prepared.Label, Public: prepared.Public,
		EncryptedSecretJSON: encrypted, ExpectedSecretRevision: &existing.SecretRevision,
		RiskLabel: prepared.RiskLabel,
	})
	if err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	if err := s.ensureConnectorRuntimeSurfacesForProfile(ctx, store, target, profile); err != nil {
		return connectortargets.CredentialProfile{}, err
	}
	return profile, nil
}
