package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s connectorTargetHandlers) listConnectorCredentialProfiles(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	if _, err := store.GetTarget(r.Context(), targetID); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	profiles, err := store.ListCredentialProfiles(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": profileSummaries(profiles)})
}

func (s connectorTargetHandlers) createConnectorCredentialProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	var request createConnectorCredentialProfileRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	preparedProfile, ok := s.prepareConnectorCredentialProfileInput(w, r, runtime, connector, createProfileAdapterRequest(request), true, nil, "")
	if !ok {
		return
	}
	var profile connectortargets.CredentialProfile
	err = s.withAuditedTransaction(r.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		txStore := connectortargets.NewTxStore(tx)
		if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
			if err := adapter.BeforeCreateCredentialProfile(r.Context(), runtime, txStore, target); err != nil {
				return err
			}
		}
		var err error
		profile, err = txStore.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
			TargetID:            target.ID,
			ConnectorKind:       target.ConnectorKind,
			Kind:                preparedProfile.Kind,
			Label:               preparedProfile.Label,
			Public:              preparedProfile.Public,
			EncryptedSecretJSON: "",
			RiskLabel:           preparedProfile.RiskLabel,
		})
		if err != nil {
			return err
		}
		encrypted, err := encryptPreparedCredentialSecret(runtime, profile.ID, preparedProfile)
		if err != nil {
			return err
		}
		if encrypted != nil {
			if err := txStore.SetCredentialProfileEncryptedSecret(r.Context(), target.ID, profile.ID, *encrypted); err != nil {
				return err
			}
			profile.EncryptedSecretJSON = *encrypted
		}
		if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), txStore, target, profile); err != nil {
			return err
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.created", map[string]any{
			"target_id": target.ID, "profile_id": profile.ID, "connector_kind": target.ConnectorKind,
			"kind": profile.Kind, "label": profile.Label,
		})
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, profileToSummary(profile))
}

func (s connectorTargetHandlers) updateConnectorCredentialProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	var request updateConnectorCredentialProfileRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	existingProfile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if lifecycle, ok := connector.(connectors.ProvisionedCredentialLifecycle); ok {
		mergedPublic, err := lifecycle.PreserveProvisionedCredentialPublic(connectortargets.CredentialProfileView(existingProfile), request.Public)
		if err != nil {
			handleConnectorTargetError(w, connectortargets.ValidationError(err.Error()))
			return
		}
		request.Public = mergedPublic
	}
	existingProfileView := connectortargets.CredentialProfileView(existingProfile)
	preparedProfile, ok := s.prepareConnectorCredentialProfileInput(w, r, runtime, connector, updateProfileAdapterRequest(request), request.Secret != nil, &existingProfileView, existingProfile.EncryptedSecretJSON)
	if !ok {
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector credential profile update was canceled")
		return
	}
	defer release()
	var profile connectortargets.CredentialProfile
	err = s.withAuditedTransaction(r.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		txStore := connectortargets.NewTxStore(tx)
		encrypted, err := encryptPreparedCredentialSecret(runtime, profileID, preparedProfile)
		if err != nil {
			return err
		}
		profile, err = txStore.UpdateCredentialProfile(r.Context(), connectortargets.UpdateCredentialProfileInput{
			TargetID:            target.ID,
			ProfileID:           profileID,
			ConnectorKind:       target.ConnectorKind,
			Kind:                preparedProfile.Kind,
			Label:               preparedProfile.Label,
			Public:              preparedProfile.Public,
			EncryptedSecretJSON: encrypted,
			RiskLabel:           preparedProfile.RiskLabel,
		})
		if err != nil {
			return err
		}
		if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), txStore, target, profile); err != nil {
			return err
		}
		return appendAudit(tx, "user", nil, 0, "connector.profile.updated", map[string]any{
			"target_id": target.ID, "profile_id": profile.ID, "connector_kind": target.ConnectorKind,
			"kind": profile.Kind, "label": profile.Label,
		})
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if err := s.afterConnectorCredentialLifecycleChange(
		r.Context(), runtime, target.ID, profile.ID,
		"connector credential profile changed; send a fresh Vault request",
		"connector credential profile was updated; ask the AI to send a fresh request", false,
	); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, profileToSummary(profile))
}

func (s connectorTargetHandlers) deleteConnectorCredentialProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	profile, err := store.GetCredentialProfile(r.Context(), targetID, profileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector credential profile deletion was canceled")
		return
	}
	defer release()
	cleanup, err := s.cleanupProvisionedCredentialProfileIfNeeded(r.Context(), runtime, target, profile)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
		if err := adapter.BeforeDeleteCredentialProfile(r.Context(), s, runtime, store, target, profile); err != nil {
			handleConnectorTargetError(w, err)
			return
		}
	}
	if err := s.withAuditedMutation(r.Context(), runtime, "user", nil, 0, "connector.profile.deleted", func() any {
		payload := map[string]any{
			"target_id": target.ID, "profile_id": profile.ID, "connector_kind": target.ConnectorKind,
			"kind": profile.Kind, "label": profile.Label,
		}
		if cleanup.Required {
			payload["external_cleanup"] = map[string]any{
				"status": cleanup.Result.Status,
				"output": cleanup.Result.Output,
			}
		}
		return payload
	}, func(tx *sql.Tx) error {
		return connectortargets.NewTxStore(tx).DeleteCredentialProfile(r.Context(), targetID, profileID)
	}); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if err := s.afterConnectorCredentialLifecycleChange(
		r.Context(), runtime, targetID, profileID,
		"connector credential profile was deleted; send a fresh Vault request",
		"connector credential profile was deleted; ask the AI to send a fresh request", true,
	); err != nil {
		writeInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s connectorTargetHandlers) testConnectorCredentialProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	loadedTarget, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	target, profile, err := store.ResolveConnectorActionTarget(r.Context(), connectortargets.ConnectorTargetRef(loadedTarget.ConnectorKind, targetID, profileID))
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if adapter := s.connectorCredentialProfileTesterFor(target.ConnectorKind); adapter != nil {
		adapter.TestCredentialProfile(s, w, r, runtime, target, profile)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	testable, ok := connector.(connectors.TestableConnector)
	if !ok {
		writeError(w, http.StatusBadRequest, "connector does not support connection tests")
		return
	}
	fullProfile, err := store.GetCredentialProfile(r.Context(), target.ID, profile.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	secrets := map[string]any{}
	if fullProfile.EncryptedSecretJSON != "" {
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, fullProfile.ID, fullProfile.EncryptedSecretJSON, &secrets); err != nil {
			writeInternalError(w)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	credentialBoundary := newConnectorCredentialBoundary(secrets)
	result, err := testable.TestConnection(ctx, connectors.RuntimeContext{
		Target:       target,
		Profile:      profile,
		Secrets:      connectorSecretAccessor{values: secrets, boundary: credentialBoundary},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
		Events:       noopConnectorEventSink{},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        string(connectors.TestUnknownError),
			Message:       credentialBoundary.Redact(s.redactForPersistence(r.Context(), runtime, err.Error())),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	redactedDetails, err := s.redactedConnectorValueWithCredentialBoundary(r.Context(), runtime, result.Details, connectorSensitiveOutputFields(), nil, credentialBoundary)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: target.ConnectorKind,
		OK:            result.Status == connectors.TestOK,
		Status:        string(result.Status),
		Message:       credentialBoundary.Redact(s.redactForPersistence(r.Context(), runtime, result.Message)),
		Details:       redactedMapValue(redactedDetails),
		DurationMS:    time.Since(start).Milliseconds(),
	})
}

func redactedMapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{"value": value}
}

func (s connectorTargetHandlers) listConnectorCredentialProfileActions(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, profile, err := connectorTargetProfileViews(r.Context(), store, targetID, profileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	actions, err := connectors.GetActionDefinitions(r.Context(), connector, target, profile)
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": actions})
}
