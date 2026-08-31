package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s connectorTargetHandlers) createConnectorTarget(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createConnectorTargetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(strings.TrimSpace(request.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), request.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Config = config
	if err := validateConnectorTargetConfig(connector, request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := connectortargets.NewStore(runtime.database)
	if err := s.validateConnectorTransportConfig(r.Context(), store, request.ProjectID, request.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	var target connectortargets.Target
	err = s.withAuditedMutation(
		r.Context(), runtime, "user", nil, 0, "connector.target.created",
		func() any {
			return map[string]any{
				"project_id": target.ProjectID, "target_id": target.ID,
				"connector_kind": target.ConnectorKind, "name": target.Name,
			}
		},
		func(tx *sql.Tx) error {
			var err error
			target, err = connectortargets.NewTxStore(tx).CreateTarget(r.Context(), connectortargets.CreateTargetInput{
				ProjectID: request.ProjectID, ConnectorKind: strings.TrimSpace(request.ConnectorKind),
				Name: request.Name, Config: request.Config,
			})
			return err
		},
	)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, connectorTargetToResponse(target, nil))
}

func (s connectorTargetHandlers) createConnectorTargetWithProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createConnectorTargetWithProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(strings.TrimSpace(request.Target.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetConfig, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), request.Target.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Target.Config = targetConfig
	if err := validateConnectorTargetConfig(connector, request.Target.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateConnectorTransportConfig(r.Context(), connectortargets.NewStore(runtime.database), request.Target.ProjectID, request.Target.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	preparedProfile, ok := s.prepareConnectorCredentialProfileInput(w, r, runtime, connector, createProfileAdapterRequest(request.Profile), true, nil, "")
	if !ok {
		return
	}
	var target connectortargets.Target
	var profile connectortargets.CredentialProfile
	err = s.withAuditedTransaction(r.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		store := connectortargets.NewTxStore(tx)
		var err error
		target, err = store.CreateTarget(r.Context(), connectortargets.CreateTargetInput{
			ProjectID:     request.Target.ProjectID,
			ConnectorKind: strings.TrimSpace(request.Target.ConnectorKind),
			Name:          request.Target.Name,
			Config:        request.Target.Config,
		})
		if err != nil {
			return err
		}
		if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
			if err := adapter.BeforeCreateCredentialProfile(r.Context(), runtime, store, target); err != nil {
				return err
			}
		}
		profile, err = store.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
			TargetID:            target.ID,
			ConnectorKind:       target.ConnectorKind,
			Kind:                preparedProfile.Kind,
			Label:               preparedProfile.Label,
			Public:              preparedProfile.Public,
			EncryptedSecretJSON: preparedProfile.EncryptedSecretJSON,
			RiskLabel:           preparedProfile.RiskLabel,
		})
		if err != nil {
			return err
		}
		if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), store, target, profile); err != nil {
			return err
		}
		if err := appendAudit(tx, "user", nil, 0, "connector.target.created", map[string]any{
			"project_id": target.ProjectID, "target_id": target.ID, "connector_kind": target.ConnectorKind, "name": target.Name,
		}); err != nil {
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
	writeJSON(w, http.StatusCreated, connectorTargetToResponse(target, []connectortargets.CredentialProfile{profile}))
}

func (s connectorTargetHandlers) updateConnectorTarget(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request updateConnectorTargetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	existing, err := store.GetTarget(r.Context(), id)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(existing.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), request.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Config = config
	if err := validateConnectorTargetConfig(connector, request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.ProjectID == 0 {
		request.ProjectID = existing.ProjectID
	}
	if err := s.validateConnectorTransportConfig(r.Context(), store, request.ProjectID, request.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector target update was canceled")
		return
	}
	defer release()
	var target connectortargets.Target
	var profiles []connectortargets.CredentialProfile
	err = s.withAuditedTransaction(r.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		txStore := connectortargets.NewTxStore(tx)
		var err error
		target, err = txStore.UpdateTarget(r.Context(), connectortargets.UpdateTargetInput{
			ID:        id,
			ProjectID: request.ProjectID,
			Name:      request.Name,
			Config:    request.Config,
		})
		if err != nil {
			return err
		}
		profiles, err = txStore.ListCredentialProfiles(r.Context(), target.ID)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), txStore, target, profile); err != nil {
				return err
			}
		}
		return appendAudit(tx, "user", nil, 0, "connector.target.updated", map[string]any{
			"project_id": target.ProjectID, "target_id": target.ID, "connector_kind": target.ConnectorKind, "name": target.Name,
		})
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if err := s.afterConnectorCredentialLifecycleChange(
		r.Context(), runtime, target.ID, 0,
		"connector target changed; send a fresh Vault request",
		"connector target was updated; ask the AI to send a fresh request", false,
	); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetToResponse(target, profiles))
}

func (s connectorTargetHandlers) updateConnectorTargetWithProfile(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	profileID, ok := parsePathInt64(w, r, "profile_id", "profile_id")
	if !ok {
		return
	}
	var request updateConnectorTargetWithProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	existing, err := store.GetTarget(r.Context(), id)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry := runtime.connectorRegistry()
	connector, ok := registry.Get(existing.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targetConfig, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), request.Target.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Target.Config = targetConfig
	if err := validateConnectorTargetConfig(connector, request.Target.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Target.ProjectID == 0 {
		request.Target.ProjectID = existing.ProjectID
	}
	if err := s.validateConnectorTransportConfig(r.Context(), store, request.Target.ProjectID, request.Target.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	existingProfile, err := store.GetCredentialProfile(r.Context(), id, profileID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	existingProfileView := connectortargets.CredentialProfileView(existingProfile)
	preparedProfile, ok := s.prepareConnectorCredentialProfileInput(w, r, runtime, connector, updateProfileAdapterRequest(request.Profile), request.Profile.Secret != nil, &existingProfileView, existingProfile.EncryptedSecretJSON)
	if !ok {
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector target update was canceled")
		return
	}
	defer release()
	var target connectortargets.Target
	var profile connectortargets.CredentialProfile
	err = s.withAuditedTransaction(r.Context(), runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
		txStore := connectortargets.NewTxStore(tx)
		var err error
		target, err = txStore.UpdateTarget(r.Context(), connectortargets.UpdateTargetInput{
			ID:        id,
			ProjectID: request.Target.ProjectID,
			Name:      request.Target.Name,
			Config:    request.Target.Config,
		})
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
			EncryptedSecretJSON: preparedProfile.EncryptedSecretPtr,
			RiskLabel:           preparedProfile.RiskLabel,
		})
		if err != nil {
			return err
		}
		if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), txStore, target, profile); err != nil {
			return err
		}
		if err := appendAudit(tx, "user", nil, 0, "connector.target.updated", map[string]any{
			"project_id": target.ProjectID, "target_id": target.ID, "connector_kind": target.ConnectorKind, "name": target.Name,
		}); err != nil {
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
		r.Context(), runtime, target.ID, 0,
		"connector target or credential profile changed; send a fresh Vault request",
		"connector target or credential profile was updated; ask the AI to send a fresh request", false,
	); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetToResponse(target, []connectortargets.CredentialProfile{profile}))
}

func (s connectorTargetHandlers) deleteConnectorTarget(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), id)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	release, err := runtime.vaultDelivery.acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "connector target deletion was canceled")
		return
	}
	defer release()
	if adapter := s.connectorTargetDeleterFor(target.ConnectorKind); adapter != nil {
		adapter.DeleteTarget(s, w, r, runtime, target)
		return
	}
	if err := s.ConnectorDeleteTargetRecord(r.Context(), runtime, target, nil); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if _, err := s.ConnectorFinalizeDeletedTarget(r.Context(), runtime, target, "connector target was deleted; ask the AI to send a fresh request", nil); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s connectorTargetHandlers) finalizeDeletedConnectorTarget(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target, staleReason string, payload map[string]any) bool {
	_, err := s.ConnectorFinalizeDeletedTarget(r.Context(), runtime, target, staleReason, payload)
	if err != nil {
		writeInternalError(w)
		return false
	}
	return true
}
