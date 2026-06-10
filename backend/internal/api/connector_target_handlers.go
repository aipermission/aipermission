package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
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
	if _, ok := registry.Get(strings.TrimSpace(request.ConnectorKind)); !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
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
		Public:              request.Public,
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
