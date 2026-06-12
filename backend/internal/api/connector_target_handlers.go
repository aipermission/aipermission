package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
)

type createConnectorTargetRequest struct {
	ConnectorKind string         `json:"connector_kind"`
	Name          string         `json:"name"`
	Config        map[string]any `json:"config,omitempty"`
}

type createConnectorCredentialProfileRequest struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

type updateConnectorTargetRequest struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

type updateConnectorCredentialProfileRequest struct {
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	Public    map[string]any `json:"public,omitempty"`
	Secret    map[string]any `json:"secret,omitempty"`
	RiskLabel string         `json:"risk_label,omitempty"`
}

type connectorTargetTestResponse struct {
	TargetID      int64          `json:"target_id"`
	ProfileID     int64          `json:"profile_id"`
	ConnectorKind string         `json:"connector_kind"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

type connectorTargetOperationRequest struct {
	ContainerRef string `json:"container_ref,omitempty"`
	Tail         int    `json:"tail,omitempty"`
}

type connectorTargetResponse struct {
	ID            int64            `json:"id"`
	Ref           string           `json:"ref,omitempty"`
	ConnectorKind string           `json:"connector_kind"`
	Name          string           `json:"name"`
	Config        map[string]any   `json:"config,omitempty"`
	Status        string           `json:"status"`
	CreatedAt     string           `json:"created_at"`
	UpdatedAt     string           `json:"updated_at"`
	Profiles      []profileSummary `json:"profiles,omitempty"`
}

type profileSummary struct {
	ID            int64          `json:"id"`
	TargetID      int64          `json:"target_id"`
	Ref           string         `json:"ref"`
	ConnectorKind string         `json:"connector_kind"`
	Kind          string         `json:"kind"`
	Label         string         `json:"label"`
	Public        map[string]any `json:"public,omitempty"`
	RiskLabel     string         `json:"risk_label,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

func validateConnectorTargetSchema(connector connectors.Connector) error {
	return connectors.ValidateNonSecretSchema(connector.TargetSchema(), connector.Kind()+" target")
}

func (s connectorTargetHandlers) listConnectorTargets(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	store := connectortargets.NewStore(runtime.database)
	targets, err := store.ListTargets(r.Context(), connectortargets.ListTargetsFilter{ConnectorKind: kind})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	items := make([]connectorTargetResponse, 0, len(targets))
	for _, target := range targets {
		items = append(items, connectorTargetToResponse(target, nil))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

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
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(strings.TrimSpace(request.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := connectors.ValidateSchemaValues(connector.TargetSchema(), request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := connectortargets.NewStore(runtime.database).CreateTarget(r.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: strings.TrimSpace(request.ConnectorKind),
		Name:          request.Name,
		Config:        request.Config,
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.created", map[string]any{
		"target_id":      target.ID,
		"connector_kind": target.ConnectorKind,
		"name":           target.Name,
	})
	writeJSON(w, http.StatusCreated, connectorTargetToResponse(target, nil))
}

func (s connectorTargetHandlers) testConnectorTargetDraft(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createConnectorTargetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(strings.TrimSpace(request.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// SSH is the built-in runtime adapter for live PTY/SFTP and host-key
	// approval. New connectors should implement TestableConnector instead of
	// adding connector-specific branches here.
	if strings.TrimSpace(request.ConnectorKind) == sshconnector.Kind {
		s.testSSHConnectorDraft(w, r, runtime, request)
		return
	}
	if err := connectors.ValidateSchemaValues(connector.TargetSchema(), request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "draft test is not supported for this connector")
}

func (s connectorTargetHandlers) getConnectorTarget(w http.ResponseWriter, r *http.Request) {
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
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetToResponse(target, profiles))
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
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(existing.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if err := validateConnectorTargetSchema(connector); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := connectors.ValidateSchemaValues(connector.TargetSchema(), request.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := store.UpdateTarget(r.Context(), connectortargets.UpdateTargetInput{
		ID:     id,
		Name:   request.Name,
		Config: request.Config,
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.updated", map[string]any{
		"target_id":      target.ID,
		"connector_kind": target.ConnectorKind,
		"name":           target.Name,
	})
	writeJSON(w, http.StatusOK, connectorTargetToResponse(target, profiles))
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
	// SSH remote authorized_keys cleanup is adapter-owned compatibility
	// behavior, not the model for new connector delete flows.
	if target.ConnectorKind == sshconnector.Kind && r.URL.Query().Get("remove_key") == "true" {
		s.deleteSSHConnectorTarget(w, r, runtime, target)
		return
	}
	if err := store.DeleteTarget(r.Context(), id); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.deleted", map[string]any{
		"target_id":      target.ID,
		"connector_kind": target.ConnectorKind,
		"name":           target.Name,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if !credentialKindSupported(connector, strings.TrimSpace(request.Kind)) {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return
	}
	schema, ok := credentialSchemaForKind(connector, strings.TrimSpace(request.Kind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, request.Public, request.Secret, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	public, err := s.canonicalCredentialPublic(r.Context(), runtime, target.ConnectorKind, strings.TrimSpace(request.Kind), request.Public)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	secret := request.Secret
	if secret == nil {
		secret = map[string]any{}
	}
	encryptedSecret, err := runtime.vault.EncryptJSON(secret)
	if err != nil {
		writeInternalError(w)
		return
	}
	profile, err := store.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                strings.TrimSpace(request.Kind),
		Label:               request.Label,
		Public:              public,
		EncryptedSecretJSON: encryptedSecret,
		RiskLabel:           request.RiskLabel,
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.profile.created", map[string]any{
		"target_id":      target.ID,
		"profile_id":     profile.ID,
		"connector_kind": target.ConnectorKind,
		"kind":           profile.Kind,
		"label":          profile.Label,
	})
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
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if !credentialKindSupported(connector, strings.TrimSpace(request.Kind)) {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return
	}
	schema, ok := credentialSchemaForKind(connector, strings.TrimSpace(request.Kind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, request.Public, request.Secret, request.Secret != nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	public, err := s.canonicalCredentialPublic(r.Context(), runtime, target.ConnectorKind, strings.TrimSpace(request.Kind), request.Public)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	var encryptedSecret *string
	if request.Secret != nil {
		encrypted, err := runtime.vault.EncryptJSON(request.Secret)
		if err != nil {
			writeInternalError(w)
			return
		}
		encryptedSecret = &encrypted
	}
	profile, err := store.UpdateCredentialProfile(r.Context(), connectortargets.UpdateCredentialProfileInput{
		TargetID:            target.ID,
		ProfileID:           profileID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                strings.TrimSpace(request.Kind),
		Label:               request.Label,
		Public:              public,
		EncryptedSecretJSON: encryptedSecret,
		RiskLabel:           request.RiskLabel,
	})
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.profile.updated", map[string]any{
		"target_id":      target.ID,
		"profile_id":     profile.ID,
		"connector_kind": target.ConnectorKind,
		"kind":           profile.Kind,
		"label":          profile.Label,
	})
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
	if err := store.DeleteCredentialProfile(r.Context(), targetID, profileID); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.profile.deleted", map[string]any{
		"target_id":      target.ID,
		"profile_id":     profile.ID,
		"connector_kind": target.ConnectorKind,
		"kind":           profile.Kind,
		"label":          profile.Label,
	})
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
	// SSH profile tests need host-key approval and gateway-managed key material.
	// Other connectors use the generic TestableConnector path below.
	if target.ConnectorKind == sshconnector.Kind {
		s.testSSHConnectorProfile(w, r, runtime, target.ID, profile.ID)
		return
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
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
		if err := runtime.vault.DecryptJSON(fullProfile.EncryptedSecretJSON, &secrets); err != nil {
			writeInternalError(w)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	result, err := testable.TestConnection(ctx, connectors.RuntimeContext{
		Target:  target,
		Profile: profile,
		Secrets: connectorSecretAccessor{values: secrets},
		Events:  noopConnectorEventSink{},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ConnectorKind: target.ConnectorKind,
			OK:            false,
			Status:        string(connectors.TestUnknownError),
			Message:       s.redactForPersistence(r.Context(), runtime, err.Error()),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: target.ConnectorKind,
		OK:            result.Status == connectors.TestOK,
		Status:        string(result.Status),
		Message:       s.redactForPersistence(r.Context(), runtime, result.Message),
		Details:       redactedMapValue(s.redactedConnectorValue(r.Context(), runtime, result.Details)),
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
	registry, err := builtin.NewRegistry()
	if err != nil {
		writeInternalError(w)
		return
	}
	connector, ok := registry.Get(target.ConnectorKind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	actions, err := connector.GetActionList(r.Context(), target, profile)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := connectors.ValidateActionDefinitions(actions, target.ConnectorKind+" actions"); err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": actions})
}

func (s connectorTargetHandlers) runConnectorTargetOperation(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	targetID, ok := parseID(w, r)
	if !ok {
		return
	}
	operation := strings.TrimSpace(r.PathValue("operation"))
	store := connectortargets.NewStore(runtime.database)
	target, err := store.GetTarget(r.Context(), targetID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	// Operations are template-launched, connector-specific conveniences. Today
	// only the SSH runtime adapter exposes Docker checks/logs here; new
	// connectors should prefer normal connector actions unless an adapter-level
	// operation is deliberately reviewed.
	if target.ConnectorKind != sshconnector.Kind {
		writeError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	mapping, err := sshRuntimeMappingForTarget(r.Context(), store, target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	server, privateKey, err := s.serverSSHMaterial(ctx, mapping.ServerID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	switch operation {
	case "docker-check":
		response, err := s.dockerCheckForServer(ctx, runtime, server, privateKey)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
			return
		}
		writeJSON(w, http.StatusOK, response)
	case "docker-logs":
		var request connectorTargetOperationRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		containerRef := strings.TrimSpace(request.ContainerRef)
		if err := validateDockerContainerRef(containerRef); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.dockerLogsForServer(ctx, runtime, server, privateKey, containerRef, request.Tail)
		if err != nil {
			if writeUnknownHostKeyError(w, err) {
				return
			}
			writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
			return
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusBadRequest, "unsupported connector operation")
	}
}

func (s connectorTargetHandlers) deleteSSHConnectorTarget(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, target connectortargets.Target) {
	store := connectortargets.NewStore(runtime.database)
	profiles, err := store.ListCredentialProfiles(r.Context(), target.ID)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if len(profiles) == 0 {
		handleConnectorTargetError(w, connectortargets.ErrTargetProfileNotFound)
		return
	}
	removedKey := false
	if r.URL.Query().Get("remove_key") == "true" {
		server, privateKey, err := s.serverSSHMaterial(r.Context(), profiles[0].ID)
		if err != nil {
			handleServerSSHMaterialError(w, err)
			return
		}
		sshKeyID := int64ConfigValue(profiles[0].Public, "ssh_key_id")
		sshKey, err := runtime.sshKeys.Get(r.Context(), sshKeyID)
		if err != nil {
			writeInternalError(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), removeAuthorizedKeyCommand(sshKey.PublicKey))
		if err != nil {
			writeError(w, http.StatusBadGateway, "remote key uninstall failed")
			return
		}
		if result.ExitCode != 0 {
			message := strings.TrimSpace(result.Stderr + result.Stdout)
			if message == "" {
				message = "remote key uninstall failed"
			}
			writeError(w, http.StatusBadGateway, message)
			return
		}
		removedKey = true
	}
	if err := store.DeleteTarget(r.Context(), target.ID); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.deleted", map[string]any{
		"target_id":          target.ID,
		"connector_kind":     sshconnector.Kind,
		"name":               target.Name,
		"remote_key_removed": removedKey,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote_key_removed": removedKey})
}

func (s connectorTargetHandlers) testSSHConnectorProfile(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, targetID int64, profileID int64) {
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	server, privateKey, err := s.serverSSHMaterial(ctx, profileID)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}
	result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			TargetID:      targetID,
			ProfileID:     profileID,
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       sshConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		TargetID:      targetID,
		ProfileID:     profileID,
		ConnectorKind: sshconnector.Kind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func (s connectorTargetHandlers) testSSHConnectorDraft(w http.ResponseWriter, r *http.Request, runtime *databaseRuntime, request createConnectorTargetRequest) {
	payload, err := s.sshConnectorPayload(r.Context(), runtime, request.Name, request.Config)
	if err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	privateKey, err := runtime.sshKeys.GetPrivateKey(r.Context(), int64ConfigValue(payload.ProfilePublic, "ssh_key_id"))
	if err != nil {
		handleSSHKeyError(w, err)
		return
	}
	const command = `printf 'aipermission-ok\n'; uname -a`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	result, err := execution.RunCommand(ctx, execution.Target{
		Host:           stringConfigValue(payload.TargetConfig, "host"),
		Port:           intConfigValue(payload.TargetConfig, "port", 22),
		Username:       stringConfigValue(payload.ProfilePublic, "username"),
		PrivateKey:     privateKey.PrivateKey,
		KnownHostsPath: s.knownHostsPath(),
	}, command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, connectorTargetTestResponse{
			ConnectorKind: sshconnector.Kind,
			OK:            false,
			Status:        "connection_failed",
			Message:       sshConnectionFailureMessage(err),
			DurationMS:    time.Since(start).Milliseconds(),
		})
		return
	}
	writeJSON(w, http.StatusOK, connectorTargetTestResponse{
		ConnectorKind: sshconnector.Kind,
		OK:            result.ExitCode == 0,
		Status:        "ok",
		Message:       strings.TrimSpace(result.Stderr + result.Stdout),
		Details: map[string]any{
			"command":   command,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"exit_code": result.ExitCode,
		},
		DurationMS: result.DurationMS,
	})
}

func sshRuntimeMappingForTarget(ctx context.Context, store *connectortargets.Store, targetID int64) (connectortargets.SSHRuntimeMapping, error) {
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return connectortargets.SSHRuntimeMapping{}, err
	}
	if len(profiles) == 0 {
		return connectortargets.SSHRuntimeMapping{}, connectortargets.ErrTargetProfileNotFound
	}
	mapping, _, _, err := store.SSHRuntimeForTargetRef(ctx, connectortargets.SSHTargetRef(targetID, profiles[0].ID))
	return mapping, err
}

type sshConnectorPayload struct {
	Name          string
	TargetConfig  map[string]any
	ProfileLabel  string
	ProfilePublic map[string]any
}

func (s connectorTargetHandlers) sshConnectorPayload(ctx context.Context, runtime *databaseRuntime, name string, config map[string]any) (sshConnectorPayload, error) {
	if config == nil {
		config = map[string]any{}
	}
	targetConfig, err := sshTargetConfigFromConnectorConfig(config)
	if err != nil {
		return sshConnectorPayload{}, err
	}
	sshConnector := sshconnector.New()
	if err := connectors.ValidateNonSecretSchema(sshConnector.TargetSchema(), "ssh target"); err != nil {
		return sshConnectorPayload{}, err
	}
	if err := connectors.ValidateSchemaValues(sshConnector.TargetSchema(), targetConfig); err != nil {
		return sshConnectorPayload{}, err
	}
	username := stringConfigValue(config, "username")
	if username == "" {
		return sshConnectorPayload{}, connectortargets.ValidationError("username is required")
	}
	profilePublic, err := s.canonicalSSHCredentialPublic(ctx, runtime, map[string]any{
		"username":   username,
		"ssh_key_id": config["ssh_key_id"],
	})
	if err != nil {
		return sshConnectorPayload{}, err
	}
	return sshConnectorPayload{
		Name:          strings.TrimSpace(name),
		TargetConfig:  targetConfig,
		ProfileLabel:  strings.TrimSpace(username),
		ProfilePublic: profilePublic,
	}, nil
}

func (s connectorTargetHandlers) canonicalCredentialPublic(ctx context.Context, runtime *databaseRuntime, connectorKind string, credentialKind string, public map[string]any) (map[string]any, error) {
	if strings.TrimSpace(connectorKind) == sshconnector.Kind {
		if strings.TrimSpace(credentialKind) != "private_key" {
			return nil, connectortargets.ValidationError("unsupported SSH credential kind")
		}
		return s.canonicalSSHCredentialPublic(ctx, runtime, public)
	}
	if public == nil {
		return map[string]any{}, nil
	}
	copied := make(map[string]any, len(public))
	for key, value := range public {
		copied[key] = value
	}
	return copied, nil
}

func (s connectorTargetHandlers) canonicalSSHCredentialPublic(ctx context.Context, runtime *databaseRuntime, public map[string]any) (map[string]any, error) {
	username := stringConfigValue(public, "username")
	if username == "" {
		return nil, connectortargets.ValidationError("username is required")
	}
	sshKeyID := int64ConfigValue(public, "ssh_key_id")
	if sshKeyID < 1 {
		return nil, connectortargets.ValidationError("ssh_key_id is required")
	}
	key, err := runtime.sshKeys.Get(ctx, sshKeyID)
	if err != nil {
		if errors.Is(err, sshkeys.ErrNotFound) {
			return nil, connectortargets.ValidationError("ssh_key_id does not reference an existing SSH credential")
		}
		return nil, err
	}
	return map[string]any{
		"username":    username,
		"ssh_key_id":  key.ID,
		"key_name":    key.Name,
		"key_type":    key.KeyType,
		"fingerprint": key.Fingerprint,
	}, nil
}

func sshTargetConfigFromConnectorConfig(config map[string]any) (map[string]any, error) {
	allowed := map[string]bool{
		"host":                        true,
		"port":                        true,
		"description":                 true,
		"startup_input_after_connect": true,
		"force_shell_command":         true,
		"username":                    true,
		"ssh_key_id":                  true,
	}
	for key := range config {
		if !allowed[key] {
			return nil, connectortargets.ValidationError("unsupported SSH connector field " + key)
		}
	}
	return map[string]any{
		"host":                        config["host"],
		"port":                        config["port"],
		"description":                 config["description"],
		"startup_input_after_connect": config["startup_input_after_connect"],
		"force_shell_command":         config["force_shell_command"],
	}, nil
}

func stringConfigValue(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(toString(value))
}

func intConfigValue(config map[string]any, key string, fallback int) int {
	value := int64ConfigValue(config, key)
	if value == 0 {
		return fallback
	}
	return int(value)
}

func int64ConfigValue(config map[string]any, key string) int64 {
	value, ok := config[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(toString(value)), 10, 64)
		return parsed
	}
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func connectorTargetToResponse(target connectortargets.Target, profiles []connectortargets.CredentialProfile) connectorTargetResponse {
	return connectorTargetResponse{
		ID:            target.ID,
		ConnectorKind: target.ConnectorKind,
		Name:          target.Name,
		Config:        target.Config,
		Status:        string(target.Status),
		CreatedAt:     target.CreatedAt,
		UpdatedAt:     target.UpdatedAt,
		Profiles:      profileSummaries(profiles),
	}
}

func profileSummaries(profiles []connectortargets.CredentialProfile) []profileSummary {
	if profiles == nil {
		return nil
	}
	items := make([]profileSummary, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, profileToSummary(profile))
	}
	return items
}

func profileToSummary(profile connectortargets.CredentialProfile) profileSummary {
	return profileSummary{
		ID:            profile.ID,
		TargetID:      profile.TargetID,
		Ref:           connectortargets.ConnectorTargetRef(profile.ConnectorKind, profile.TargetID, profile.ID),
		ConnectorKind: profile.ConnectorKind,
		Kind:          profile.Kind,
		Label:         profile.Label,
		Public:        profile.Public,
		RiskLabel:     profile.RiskLabel,
		CreatedAt:     profile.CreatedAt,
		UpdatedAt:     profile.UpdatedAt,
	}
}

func credentialKindSupported(connector connectors.Connector, kind string) bool {
	if !connectors.ValidIdentifier(kind) {
		return false
	}
	for _, schema := range connector.CredentialSchemas() {
		if schema.Kind == kind {
			return true
		}
	}
	return false
}

func credentialSchemaForKind(connector connectors.Connector, kind string) (connectors.CredentialSchema, bool) {
	if !connectors.ValidIdentifier(kind) {
		return connectors.CredentialSchema{}, false
	}
	for _, schema := range connector.CredentialSchemas() {
		if schema.Kind == kind {
			return schema, true
		}
	}
	return connectors.CredentialSchema{}, false
}

func handleConnectorTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeError(w, http.StatusNotFound, "connector target not found")
	case errors.Is(err, connectortargets.ErrInvalidTargetRef):
		writeError(w, http.StatusBadRequest, "invalid connector target ref")
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	default:
		writeInternalError(w)
	}
}
