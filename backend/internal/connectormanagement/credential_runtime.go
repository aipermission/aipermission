package connectormanagement

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorcredentials"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type CredentialRuntimePorts = connectorcredentials.RuntimePorts

type ManagedCredentialCleanupScope struct {
	Database             *sql.DB
	Registry             connectors.Catalog
	Runtime              CredentialRuntimePorts
	EvidenceCapabilities func(string) (connectors.RuntimeCapabilityResolver, error)
}

func CleanupProvisionedCredentialProfileIfNeeded(
	ctx context.Context,
	scope ManagedCredentialCleanupScope,
	target connectortargets.Target,
	profile connectortargets.CredentialProfile,
) (ProfileCleanupOutcome, error) {
	if scope.Database == nil || scope.Registry == nil || !scope.Runtime.Valid() {
		return ProfileCleanupOutcome{}, errProfileDeletionRuntimeUnavailable
	}
	connector, ok := scope.Registry.Get(target.ConnectorKind)
	if !ok {
		return ProfileCleanupOutcome{}, connectortargets.ValidationError("unsupported connector kind")
	}
	lifecycle, ok := connector.(connectors.ProvisionedCredentialLifecycle)
	if !ok {
		return ProfileCleanupOutcome{}, nil
	}
	adminProfileID, managed, err := lifecycle.ProvisionedCredentialAdminProfileID(connectortargets.CredentialProfileView(profile))
	if err != nil {
		return ProfileCleanupOutcome{}, connectortargets.ValidationError(err.Error())
	}
	if !managed {
		return ProfileCleanupOutcome{}, nil
	}
	provisioner, ok := connector.(connectors.CredentialProvisioner)
	if !ok {
		return ProfileCleanupOutcome{}, connectortargets.ValidationError("connector does not support managed credential cleanup")
	}
	if outcome, handled, err := completedCredentialCleanupEvidence(ctx, scope, connector, target, profile); handled {
		return outcome, err
	}
	adminProfile, err := connectortargets.NewStore(scope.Database).GetCredentialProfile(ctx, target.ID, adminProfileID)
	if err != nil {
		return ProfileCleanupOutcome{}, err
	}
	adminSecrets, err := decryptCredentialSecrets(ctx, adminProfile, scope.Runtime.DecryptSecret)
	if err != nil {
		return ProfileCleanupOutcome{}, fmt.Errorf("decrypt admin profile secret: %w", err)
	}
	profileSecrets, err := decryptCredentialSecrets(ctx, profile, scope.Runtime.DecryptSecret)
	if err != nil {
		return ProfileCleanupOutcome{}, fmt.Errorf("decrypt managed profile secret: %w", err)
	}
	boundary := actionresult.CombinedCredentialBoundary(adminSecrets, profileSecrets)
	result, err := provisioner.CleanupProvisionedCredentialProfile(
		ctx,
		scope.Runtime.RuntimeContext(target, adminProfile, adminSecrets, boundary),
		connectortargets.CredentialProfileView(profile),
	)
	return completedCredentialCleanupOutcome(ctx, scope.Runtime, result, err, boundary)
}

func decryptCredentialSecrets(
	ctx context.Context,
	profile connectortargets.CredentialProfile,
	decrypt func(context.Context, int64, string) (map[string]any, error),
) (map[string]any, error) {
	if profile.EncryptedSecretJSON == "" {
		return map[string]any{}, nil
	}
	return decrypt(ctx, profile.ID, profile.EncryptedSecretJSON)
}
