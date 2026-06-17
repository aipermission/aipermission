package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type provisionConnectorCredentialProfileRequest struct {
	Input map[string]any `json:"input,omitempty"`
}

type provisionConnectorCredentialProfileResponse struct {
	Profile profileSummary          `json:"profile"`
	Result  connectors.ActionResult `json:"result"`
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
	provisioned, err := provisioner.ProvisionCredentialProfile(r.Context(), connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
	}, request.Input)
	if err != nil {
		handleConnectorProvisionError(w, err)
		return
	}
	if err := validateProvisionedCredentialProfile(connector, provisioned); err != nil {
		cleanupProvisionedCredentialProfileAfterFailure(provisioner, target, adminProfile, secrets, provisioned, s.Server, runtime)
		handleConnectorTargetError(w, err)
		return
	}
	if profileLabelExists(r.Context(), store, target.ID, provisioned.Label) {
		cleanupProvisionedCredentialProfileAfterFailure(provisioner, target, adminProfile, secrets, provisioned, s.Server, runtime)
		handleConnectorTargetError(w, connectortargets.ValidationError("connector profile label already exists"))
		return
	}
	encrypted, err := runtime.vault.EncryptJSON(provisioned.Secret)
	if err != nil {
		writeInternalError(w)
		return
	}
	profile, err := store.CreateCredentialProfile(r.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       target.ConnectorKind,
		Kind:                provisioned.Kind,
		Label:               provisioned.Label,
		Public:              provisioned.Public,
		EncryptedSecretJSON: encrypted,
		RiskLabel:           provisioned.RiskLabel,
	})
	if err != nil {
		cleanupProvisionedCredentialProfileAfterFailure(provisioner, target, adminProfile, secrets, provisioned, s.Server, runtime)
		handleConnectorTargetError(w, err)
		return
	}
	if err := s.ensureConnectorRuntimeSurfacesForProfile(r.Context(), store, target, profile); err != nil {
		writeInternalError(w)
		return
	}
	s.writeAudit(r.Context(), runtime, "user", nil, 0, "connector.profile.provisioned", map[string]any{
		"target_id":        target.ID,
		"profile_id":       profile.ID,
		"admin_profile_id": adminProfile.ID,
		"connector_kind":   target.ConnectorKind,
		"kind":             profile.Kind,
		"label":            profile.Label,
	})
	writeJSON(w, http.StatusCreated, provisionConnectorCredentialProfileResponse{
		Profile: profileToSummary(profile),
		Result:  provisioned.Result,
	})
}

func cleanupProvisionedCredentialProfileAfterFailure(
	provisioner connectors.CredentialProvisioner,
	target connectortargets.Target,
	adminProfile connectortargets.CredentialProfile,
	secrets map[string]any,
	provisioned connectors.ProvisionedCredentialProfile,
	server *Server,
	runtime *databaseRuntime,
) {
	if provisioner == nil {
		return
	}
	_, _ = provisioner.CleanupProvisionedCredentialProfile(context.Background(), connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, server, runtime),
	}, connectors.CredentialProfileView{
		ID:            0,
		TargetID:      target.ID,
		ConnectorKind: target.ConnectorKind,
		Kind:          provisioned.Kind,
		Label:         provisioned.Label,
		Public:        provisioned.Public,
		RiskLabel:     provisioned.RiskLabel,
	})
}

func (s connectorTargetHandlers) cleanupProvisionedCredentialProfileIfNeeded(ctx context.Context, runtime *databaseRuntime, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	if !boolMapValue(profile.Public, "managed_by_aipermission") {
		return nil
	}
	connector, ok := runtime.connectorRegistry().Get(target.ConnectorKind)
	if !ok {
		return connectortargets.ValidationError("unsupported connector kind")
	}
	provisioner, ok := connector.(connectors.CredentialProvisioner)
	if !ok {
		return connectortargets.ValidationError("connector does not support managed credential cleanup")
	}
	adminProfileID := int64MapValue(profile.Public, "managed_admin_profile_id")
	if adminProfileID < 1 || adminProfileID == profile.ID {
		return connectortargets.ValidationError("managed credential profile is missing a valid admin profile reference")
	}
	store := connectortargets.NewStore(runtime.database)
	adminProfile, err := store.GetCredentialProfile(ctx, target.ID, adminProfileID)
	if err != nil {
		return err
	}
	secrets := map[string]any{}
	if adminProfile.EncryptedSecretJSON != "" {
		if err := runtime.vault.DecryptJSON(adminProfile.EncryptedSecretJSON, &secrets); err != nil {
			return fmt.Errorf("decrypt admin profile secret: %w", err)
		}
	}
	_, err = provisioner.CleanupProvisionedCredentialProfile(ctx, connectors.RuntimeContext{
		Target:       connectorTargetViewForProfile(target, adminProfile.ID),
		Profile:      connectortargets.CredentialProfileView(adminProfile),
		Secrets:      connectorSecretAccessor{values: secrets},
		Events:       noopConnectorEventSink{},
		Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s.Server, runtime),
	}, connectortargets.CredentialProfileView(profile))
	return err
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
	if err := runtime.vault.DecryptJSON(profile.EncryptedSecretJSON, &secrets); err != nil {
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

func profileLabelExists(ctx context.Context, store *connectortargets.Store, targetID int64, label string) bool {
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return false
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Label), strings.TrimSpace(label)) {
			return true
		}
	}
	return false
}

func handleConnectorProvisionError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func boolMapValue(values map[string]any, name string) bool {
	if values == nil {
		return false
	}
	switch typed := values[name].(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func int64MapValue(values map[string]any, name string) int64 {
	if values == nil {
		return 0
	}
	switch typed := values[name].(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
