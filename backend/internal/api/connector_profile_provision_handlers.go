package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type provisionConnectorCredentialProfileRequest struct {
	Input map[string]any `json:"input,omitempty"`
}

type provisionConnectorCredentialProfileResponse struct {
	Profile profileSummary          `json:"profile"`
	Result  connectors.ActionResult `json:"result"`
}

const (
	provisionCompensationTimeout = 15 * time.Second
	provisionAuditTimeout        = 5 * time.Second
)

type provisionCompensationOutcome struct {
	cleanupErr error
	auditErr   error
}

func (s connectorTargetHandlers) provisionConnectorCredentialProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	adminProfileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	var request provisionConnectorCredentialProfileRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	adminProfile, err := store.GetCredentialProfile(r.Context(), targetID, adminProfileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	connector, ok := runtime.connectorRegistry().Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	provisioner, ok := connector.(connectors.CredentialProvisioner)
	if !ok {
		writeError(w, http.StatusBadRequest, "connector does not support credential provisioning")
		return
	}
	secrets, ok := s.decryptConnectorProfileSecrets(w, runtime, adminProfile)
	if !ok {
		return
	}
	credentialBoundary := newConnectorCredentialBoundary(secrets)
	provisioned, err := provisioner.ProvisionCredentialProfile(r.Context(), connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets, boundary: credentialBoundary},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
	}, request.Input)
	if err != nil {
		handleConnectorProvisionError(w, errors.New(credentialBoundary.Redact(s.redactForPersistence(r.Context(), runtime, err.Error()))))
		return
	}
	credentialBoundary.AddStructured(provisioned.Secret)
	if err := validateProvisionedCredentialProfile(connector, provisioned); err != nil {
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "validation", err, func() {
			handleConnectorTargetError(w, err)
		})
		return
	}
	redactedResult, err := s.redactConnectorActionResultWithCredentialBoundary(r.Context(), runtime, provisioned.Result, credentialBoundary)
	if err != nil {
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "result_redaction", err, func() {
			writeInternalError(w)
		})
		return
	}
	provisioned.Result = redactedResult
	labelExists, err := profileLabelExists(r.Context(), store, target.ID, provisioned.Label)
	if err != nil {
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "profile_label_lookup", err, func() {
			handleConnectorTargetError(w, err)
		})
		return
	}
	if labelExists {
		err := connectortargets.ValidationError("connector profile label already exists")
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "duplicate_profile_label", err, func() {
			handleConnectorTargetError(w, err)
		})
		return
	}
	provisionedSecretJSON, err := json.Marshal(provisioned.Secret)
	if err != nil {
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "secret_encryption", err, func() {
			writeInternalError(w)
		})
		return
	}
	var profile connectortargets.CredentialProfile
	err = s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "connector.profile.provisioned",
		func() any {
			return map[string]any{
				"target_id": target.ID, "profile_id": profile.ID,
				"admin_profile_id": adminProfile.ID, "connector_kind": target.ConnectorKind,
				"kind": profile.Kind, "label": profile.Label,
			}
		},
		func(tx *sql.Tx) error {
			txStore := connectortargets.NewTxStore(tx)
			var err error
			profile, err = txStore.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
				TargetID:            target.ID,
				ConnectorKind:       target.ConnectorKind,
				Kind:                provisioned.Kind,
				Label:               provisioned.Label,
				Public:              provisioned.Public,
				EncryptedSecretJSON: "",
				RiskLabel:           provisioned.RiskLabel,
			})
			if err != nil {
				return err
			}
			encrypted, err := recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, json.RawMessage(provisionedSecretJSON))
			if err != nil {
				return fmt.Errorf("encrypt provisioned credential profile: %w", err)
			}
			if err := txStore.SetCredentialProfileEncryptedSecret(r.Context(), target.ID, profile.ID, encrypted); err != nil {
				return err
			}
			profile.EncryptedSecretJSON = encrypted
			return s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), txStore, target, profile)
		},
	)
	if err != nil {
		s.failProvisionedCredentialProfile(w, runtime, provisioner, target, adminProfile, secrets, provisioned, "profile_persistence", err, func() {
			handleConnectorTargetError(w, err)
		})
		return
	}
	writeJSON(w, http.StatusCreated, provisionConnectorCredentialProfileResponse{
		Profile: profileToSummary(profile),
		Result:  provisioned.Result,
	})
}

func (s connectorTargetHandlers) failProvisionedCredentialProfile(
	w http.ResponseWriter,
	runtime *databaseRuntime,
	provisioner connectors.CredentialProvisioner,
	target connectortargets.Target,
	adminProfile connectortargets.CredentialProfile,
	secrets map[string]any,
	provisioned connectors.ProvisionedCredentialProfile,
	stage string,
	cause error,
	respond func(),
) {
	outcome := s.compensateProvisionedCredentialProfile(provisioner, target, adminProfile, secrets, provisioned, runtime, stage, cause)
	if outcome.cleanupErr != nil {
		log.Printf("credential provisioning compensation failed connector=%q target_id=%d stage=%q", target.ConnectorKind, target.ID, stage)
		writeErrorWithCode(
			w,
			http.StatusInternalServerError,
			"credential provisioning failed and remote cleanup could not be confirmed; review the remote service before retrying",
			"provisioning_reconciliation_required",
		)
		return
	}
	if outcome.auditErr != nil {
		log.Printf("credential provisioning compensation audit failed connector=%q target_id=%d stage=%q", target.ConnectorKind, target.ID, stage)
		writeErrorWithCode(
			w,
			http.StatusInternalServerError,
			"credential provisioning cleanup completed but its audit record could not be persisted; review audit health before retrying",
			"provisioning_compensation_audit_failed",
		)
		return
	}
	respond()
}

func (s connectorTargetHandlers) compensateProvisionedCredentialProfile(
	provisioner connectors.CredentialProvisioner,
	target connectortargets.Target,
	adminProfile connectortargets.CredentialProfile,
	secrets map[string]any,
	provisioned connectors.ProvisionedCredentialProfile,
	runtime *databaseRuntime,
	stage string,
	cause error,
) provisionCompensationOutcome {
	if provisioner == nil {
		return provisionCompensationOutcome{cleanupErr: fmt.Errorf("credential provisioner is unavailable")}
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), provisionCompensationTimeout)
	credentialBoundary := combinedConnectorCredentialBoundary(secrets, provisioned.Secret)
	cleanupResult, cleanupErr := provisioner.CleanupProvisionedCredentialProfile(cleanupCtx, connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets, boundary: credentialBoundary},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
	}, connectors.CredentialProfileView{
		ID:            0,
		TargetID:      target.ID,
		ConnectorKind: target.ConnectorKind,
		Kind:          provisioned.Kind,
		Label:         provisioned.Label,
		Public:        provisioned.Public,
		RiskLabel:     provisioned.RiskLabel,
	})
	cleanupCancel()
	cleanupErr = requireCompletedCredentialCleanup(cleanupResult, cleanupErr)
	action := "connector.profile.provisioning_compensated"
	cleanupStatus := "completed"
	if cleanupErr != nil {
		action = "connector.profile.provisioning_reconciliation_required"
		cleanupStatus = "failed"
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), provisionAuditTimeout)
	auditErr := s.writeAuditRequired(auditCtx, runtime, "gateway", nil, 0, action, map[string]any{
		"target_id":        target.ID,
		"admin_profile_id": adminProfile.ID,
		"connector_kind":   target.ConnectorKind,
		"kind":             provisioned.Kind,
		"label":            provisioned.Label,
		"failure_stage":    stage,
		"failure":          provisionErrorMessage(credentialBoundary, cause),
		"cleanup_status":   cleanupStatus,
		"cleanup_error":    provisionErrorMessage(credentialBoundary, cleanupErr),
	})
	auditCancel()
	return provisionCompensationOutcome{cleanupErr: cleanupErr, auditErr: auditErr}
}

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
	credentialBoundary := combinedConnectorCredentialBoundary(secrets, profileSecrets)
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
	if err != nil {
		return err
	}
	if result.Status != connectors.ResultCompleted {
		return fmt.Errorf("credential cleanup returned status %q", result.Status)
	}
	return nil
}

func connectorTargetViewForProfile(target connectortargets.Target, profileID int64) connectors.TargetView {
	return connectors.TargetView{
		ID:            target.ID,
		Ref:           connectortargets.ConnectorTargetRef(target.ConnectorKind, target.ID, profileID),
		ConnectorKind: target.ConnectorKind,
		Name:          target.Name,
		Config:        cloneMapAny(target.Config),
	}
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

func validateProvisionedCredentialProfile(connector connectors.Connector, profile connectors.ProvisionedCredentialProfile) error {
	if !credentialKindSupported(connector, profile.Kind) {
		return connectortargets.ValidationError("unsupported credential kind")
	}
	schema, ok := credentialSchemaForKind(connector, profile.Kind)
	if !ok {
		return connectortargets.ValidationError("unsupported credential kind")
	}
	if strings.TrimSpace(profile.Label) == "" {
		return connectortargets.ValidationError("profile label is required")
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, profile.Public, profile.Secret, true); err != nil {
		return connectortargets.ValidationError(err.Error())
	}
	return nil
}

func profileLabelExists(ctx context.Context, store *connectortargets.Store, targetID int64, label string) (bool, error) {
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return false, fmt.Errorf("list connector credential profiles: %w", err)
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Label), strings.TrimSpace(label)) {
			return true, nil
		}
	}
	return false, nil
}

func provisionErrorMessage(boundary connectorCredentialBoundary, err error) string {
	if err == nil {
		return ""
	}
	return boundary.Redact(redactBasic(err.Error()))
}

func handleConnectorProvisionError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
