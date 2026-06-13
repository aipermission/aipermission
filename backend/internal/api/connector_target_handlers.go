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
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/history"
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
	ProfileID    int64  `json:"profile_id,omitempty"`
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
	if adapter := connectorDraftTesterFor(request.ConnectorKind); adapter != nil {
		adapter.TestDraft(s, w, r, runtime, request)
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
	if adapter := connectorTargetDeleterFor(target.ConnectorKind); adapter != nil {
		adapter.DeleteTarget(s, w, r, runtime, target)
		return
	}
	staleRequests, err := s.staleConnectorActionRequestsForTarget(r.Context(), runtime, id, 0, "connector target was deleted; ask the AI to send a fresh request")
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := store.DeleteTarget(r.Context(), id); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.target.deleted", map[string]any{
		"target_id":                target.ID,
		"connector_kind":           target.ConnectorKind,
		"name":                     target.Name,
		"stale_connector_requests": staleRequests,
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
	if adapter := connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
		if err := adapter.BeforeCreateCredentialProfile(r.Context(), runtime, store, target); err != nil {
			handleConnectorTargetError(w, err)
			return
		}
	}
	registry := runtime.connectorRegistry()
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
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, public, request.Secret, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	registry := runtime.connectorRegistry()
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
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, public, request.Secret, request.Secret != nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if adapter := connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
		if err := adapter.BeforeDeleteCredentialProfile(r.Context(), s, runtime, store, target, profile); err != nil {
			handleConnectorTargetError(w, err)
			return
		}
	}
	staleRequests, err := s.staleConnectorActionRequestsForTarget(r.Context(), runtime, targetID, profileID, "connector credential profile was deleted; ask the AI to send a fresh request")
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := store.DeleteCredentialProfile(r.Context(), targetID, profileID); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.profile.deleted", map[string]any{
		"target_id":                target.ID,
		"profile_id":               profile.ID,
		"connector_kind":           target.ConnectorKind,
		"kind":                     profile.Kind,
		"label":                    profile.Label,
		"stale_connector_requests": staleRequests,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s connectorTargetHandlers) staleConnectorActionRequestsForTarget(ctx context.Context, runtime *databaseRuntime, targetID int64, profileID int64, reason string) (int64, error) {
	if runtime == nil || runtime.database == nil || targetID < 1 {
		return 0, nil
	}
	store := connectortargets.NewStore(runtime.database)
	result, err := store.StaleActionRequestsForTarget(ctx, connectortargets.StaleActionRequestsForTargetInput{
		TargetID:  targetID,
		ProfileID: profileID,
		Error:     s.redactForPersistence(ctx, runtime, reason),
	})
	if err != nil {
		return 0, err
	}
	if len(result.IDs) == 0 {
		return 0, nil
	}
	historyStore := history.NewStore(runtime.database)
	for _, id := range result.IDs {
		if err := historyStore.SyncConnectorActionRequest(ctx, id); err != nil {
			return 0, err
		}
	}
	return result.Affected, nil
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
	if adapter := connectorCredentialProfileTesterFor(target.ConnectorKind); adapter != nil {
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
		if err := runtime.vault.DecryptJSON(fullProfile.EncryptedSecretJSON, &secrets); err != nil {
			writeInternalError(w)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	result, err := testable.TestConnection(ctx, connectors.RuntimeContext{
		Target:   target,
		Profile:  profile,
		Secrets:  connectorSecretAccessor{values: secrets},
		Services: connectorRuntimeServices(target.ConnectorKind, s.Server, runtime),
		Events:   noopConnectorEventSink{},
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
		Details:       redactedMapValue(s.redactedConnectorValue(r.Context(), runtime, result.Details, connectorSensitiveOutputFields())),
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
	adapter := connectorTargetOperationRunnerFor(target.ConnectorKind)
	if adapter == nil {
		writeError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	adapter.RunTargetOperation(s, w, r, runtime, target, operation)
}

func (s connectorTargetHandlers) canonicalCredentialPublic(ctx context.Context, runtime *databaseRuntime, connectorKind string, credentialKind string, public map[string]any) (map[string]any, error) {
	if adapter := connectorCredentialCanonicalizerFor(connectorKind); adapter != nil {
		return adapter.CanonicalCredentialPublic(ctx, s, runtime, credentialKind, public)
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
