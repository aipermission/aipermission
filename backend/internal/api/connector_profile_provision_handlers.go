package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type credentialCleanupOutcome struct {
	Required bool
	Result   connectors.ActionResult
}

func (s connectorTargetHandlers) cleanupProvisionedCredentialProfileIfNeeded(ctx context.Context, runtime *databaseRuntime, target connectortargets.Target, profile connectortargets.CredentialProfile) (credentialCleanupOutcome, error) {
	connector, ok := runtime.connectorRegistry().Get(target.ConnectorKind)
	if !ok {
		return credentialCleanupOutcome{}, connectortargets.ValidationError("unsupported connector kind")
	}
	lifecycle, ok := connector.(connectors.ProvisionedCredentialLifecycle)
	if !ok {
		return credentialCleanupOutcome{}, nil
	}
	adminProfileID, managed, err := lifecycle.ProvisionedCredentialAdminProfileID(connectortargets.CredentialProfileView(profile))
	if err != nil {
		return credentialCleanupOutcome{}, connectortargets.ValidationError(err.Error())
	}
	if !managed {
		return credentialCleanupOutcome{}, nil
	}
	provisioner, ok := connector.(connectors.CredentialProvisioner)
	if !ok {
		return credentialCleanupOutcome{}, connectortargets.ValidationError("connector does not support managed credential cleanup")
	}
	store := connectortargets.NewStore(runtime.database)
	adminProfile, err := store.GetCredentialProfile(ctx, target.ID, adminProfileID)
	if err != nil {
		return credentialCleanupOutcome{}, err
	}
	secrets := map[string]any{}
	if adminProfile.EncryptedSecretJSON != "" {
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, adminProfile.ID, adminProfile.EncryptedSecretJSON, &secrets); err != nil {
			return credentialCleanupOutcome{}, fmt.Errorf("decrypt admin profile secret: %w", err)
		}
	}
	profileSecrets := map[string]any{}
	if profile.EncryptedSecretJSON != "" {
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &profileSecrets); err != nil {
			return credentialCleanupOutcome{}, fmt.Errorf("decrypt managed profile secret: %w", err)
		}
	}
	credentialBoundary := actions.CombinedCredentialBoundary(secrets, profileSecrets)
	result, err := provisioner.CleanupProvisionedCredentialProfile(ctx, connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets, boundary: credentialBoundary},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
	}, connectortargets.CredentialProfileView(profile))
	if err := requireCompletedCredentialCleanup(result, err); err != nil {
		return credentialCleanupOutcome{}, errors.New(credentialBoundary.Redact(s.redactForPersistence(ctx, runtime, err.Error())))
	}
	redacted, err := s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, credentialBoundary)
	if err != nil {
		return credentialCleanupOutcome{}, fmt.Errorf("process credential cleanup result: %w", err)
	}
	return credentialCleanupOutcome{Required: true, Result: redacted}, nil
}

func requireCompletedCredentialCleanup(result connectors.ActionResult, err error) error {
	return connectormanagement.RequireCompletedCredentialCleanup(result, err)
}

func connectorTargetViewForProfile(target connectortargets.Target, profileID int64) connectors.TargetView {
	return connectors.TargetView{
		ID:            target.ID,
		Ref:           connectors.FormatTargetRef(target.ConnectorKind, target.ID, profileID),
		ConnectorKind: target.ConnectorKind,
		Name:          target.Name,
		Config:        cloneConnectorMap(target.Config),
	}
}

func cloneConnectorMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (s connectorTargetHandlers) decryptConnectorProfileSecrets(w http.ResponseWriter, runtime *databaseRuntime, profile connectortargets.CredentialProfile) (map[string]any, bool) {
	secrets := map[string]any{}
	if profile.EncryptedSecretJSON == "" {
		return secrets, true
	}
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		writeInternalError(w)
		return nil, false
	}
	return secrets, true
}

func handleConnectorProvisionError(w http.ResponseWriter, err error, safeMessage string) {
	if err == nil {
		return
	}
	status := http.StatusBadRequest
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		status = http.StatusConflict
	}
	writeErrorWithCode(w, status, safeMessage, connectors.ErrorCode(err))
}
