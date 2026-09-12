package connectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type CredentialRuntimePorts struct {
	DecryptSecret  func(context.Context, int64, string) (map[string]any, error)
	RuntimeContext func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, CredentialBoundary) connectors.RuntimeContext
	RedactResult   func(context.Context, connectors.ActionResult, CredentialBoundary) (connectors.ActionResult, error)
	RedactText     func(context.Context, string) string
}

func (ports CredentialRuntimePorts) valid() bool {
	return ports.DecryptSecret != nil && ports.RuntimeContext != nil && ports.RedactResult != nil && ports.RedactText != nil
}

type ManagedCredentialCleanupScope struct {
	Database *sql.DB
	Registry connectors.Catalog
	Runtime  CredentialRuntimePorts
}

func CleanupProvisionedCredentialProfileIfNeeded(
	ctx context.Context,
	scope ManagedCredentialCleanupScope,
	target connectortargets.Target,
	profile connectortargets.CredentialProfile,
) (ProfileCleanupOutcome, error) {
	if scope.Database == nil || scope.Registry == nil || !scope.Runtime.valid() {
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
	if err := RequireCompletedCredentialCleanup(result, err); err != nil {
		return ProfileCleanupOutcome{}, errors.New(boundary.Redact(scope.Runtime.RedactText(ctx, err.Error())))
	}
	redacted, err := scope.Runtime.RedactResult(ctx, result, boundary)
	if err != nil {
		return ProfileCleanupOutcome{}, fmt.Errorf("process credential cleanup result: %w", err)
	}
	return ProfileCleanupOutcome{Required: true, Status: string(redacted.Status), Output: redacted.Output}, nil
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
