package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func validateConnectorTargetSchema(connector connectors.Connector) error {
	return connectors.ValidateNonSecretSchema(connector.TargetSchema(), connector.Kind()+" target")
}

func validateConnectorTargetConfig(connector connectors.Connector, config map[string]any) error {
	validator, ok := connector.(connectors.TargetConfigValidator)
	if !ok {
		return nil
	}
	return validator.ValidateTargetConfig(config)
}

func normalizeConnectorTargetUpdate(connector connectors.Connector, existing, submitted map[string]any) (map[string]any, error) {
	if normalizer, ok := connector.(connectors.TargetConfigUpdateNormalizer); ok {
		submitted = normalizer.NormalizeTargetConfigUpdate(existing, submitted)
	}
	return connectors.NormalizeSchemaValues(connector.TargetSchema(), submitted)
}

func (s *Server) validateConnectorTransportConfig(ctx context.Context, store *connectortargets.Store, projectID int64, config map[string]any) error {
	mode, _ := config["connection_mode"].(string)
	if strings.TrimSpace(mode) != "over_ssh" {
		return nil
	}
	transportTargetRef, _ := config["transport_target_ref"].(string)
	transportTargetRef = strings.TrimSpace(transportTargetRef)
	if transportTargetRef == "" {
		return connectortargets.ValidationError("transport target ref is required for over_ssh")
	}
	if err := store.ValidateTransportProject(ctx, projectID, transportTargetRef); err != nil {
		return err
	}
	kind, _, _, ok := connectortargets.ParseConnectorTargetRef(transportTargetRef)
	if !ok {
		return connectortargets.ErrInvalidTargetRef
	}
	if adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter); adapter == nil {
		return connectortargets.ValidationError(fmt.Sprintf("%s connector does not expose reviewed TCP transport", kind))
	}
	return nil
}

type preparedConnectorCredentialProfileInput struct {
	Kind          string
	Label         string
	Public        map[string]any
	Secret        map[string]any
	SecretChanged bool
	RiskLabel     string
}

type connectorCredentialProfilePayload struct {
	Kind      string
	Label     string
	Public    map[string]any
	Secret    map[string]any
	RiskLabel string
}

func createProfileAdapterRequest(request createConnectorCredentialProfileRequest) connectorCredentialProfilePayload {
	return connectorCredentialProfilePayload{
		Kind:      request.Kind,
		Label:     request.Label,
		Public:    request.Public,
		Secret:    request.Secret,
		RiskLabel: request.RiskLabel,
	}
}

func updateProfileAdapterRequest(request updateConnectorCredentialProfileRequest) connectorCredentialProfilePayload {
	return connectorCredentialProfilePayload{
		Kind:      request.Kind,
		Label:     request.Label,
		Public:    request.Public,
		Secret:    request.Secret,
		RiskLabel: request.RiskLabel,
	}
}

func (s connectorTargetHandlers) prepareConnectorCredentialProfileInput(
	w http.ResponseWriter,
	r *http.Request,
	runtime *databaseRuntime,
	connector connectors.Connector,
	request connectorCredentialProfilePayload,
	secretRequired bool,
	previous *connectors.CredentialProfileView,
	previousEncryptedSecret string,
) (preparedConnectorCredentialProfileInput, bool) {
	kind := strings.TrimSpace(request.Kind)
	if !credentialKindSupported(connector, kind) {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return preparedConnectorCredentialProfileInput{}, false
	}
	schema, ok := credentialSchemaForKind(connector, kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported credential kind")
		return preparedConnectorCredentialProfileInput{}, false
	}
	secret, err := mergeConnectorCredentialSecrets(runtime, previous, previousEncryptedSecret, request.Secret)
	if err != nil {
		writeInternalError(w)
		return preparedConnectorCredentialProfileInput{}, false
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, request.Public, secret, secretRequired); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return preparedConnectorCredentialProfileInput{}, false
	}
	public, err := s.canonicalCredentialPublic(r.Context(), runtime, connector.Kind(), kind, request.Public)
	if err != nil {
		handleConnectorTargetError(w, err)
		return preparedConnectorCredentialProfileInput{}, false
	}
	if err := connectors.ValidateCredentialSchemaValues(schema.Schema, public, secret, secretRequired); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return preparedConnectorCredentialProfileInput{}, false
	}
	if validator, ok := connector.(connectors.CredentialProfileValidator); ok {
		if err := validator.ValidateCredentialProfile(kind, public, secret, previous); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return preparedConnectorCredentialProfileInput{}, false
		}
	}
	prepared := preparedConnectorCredentialProfileInput{
		Kind:      kind,
		Label:     request.Label,
		Public:    public,
		RiskLabel: request.RiskLabel,
	}
	if secretRequired {
		if secret == nil {
			secret = map[string]any{}
		}
		prepared.Secret = secret
		prepared.SecretChanged = true
		return prepared, true
	}
	if request.Secret != nil {
		prepared.Secret = secret
		prepared.SecretChanged = true
	}
	return prepared, true
}

func mergeConnectorCredentialSecrets(runtime *databaseRuntime, previous *connectors.CredentialProfileView, previousEncrypted string, updates map[string]any) (map[string]any, error) {
	if updates == nil && previousEncrypted == "" {
		return nil, nil
	}
	merged := map[string]any{}
	if previousEncrypted != "" {
		if previous == nil || previous.ID < 1 {
			return nil, fmt.Errorf("previous credential profile identity is required")
		}
		if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, previous.ID, previousEncrypted, &merged); err != nil {
			return nil, err
		}
	}
	for key, value := range updates {
		if value == nil {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return merged, nil
}

func encryptPreparedCredentialSecret(runtime *databaseRuntime, profileID int64, prepared preparedConnectorCredentialProfileInput) (*string, error) {
	if !prepared.SecretChanged {
		return nil, nil
	}
	encrypted, err := recordcrypto.EncryptJSON(
		runtime.vault,
		runtime.workspaceUUID,
		recordcrypto.ConnectorCredentialProfile,
		profileID,
		prepared.Secret,
	)
	if err != nil {
		return nil, fmt.Errorf("encrypt connector credential profile: %w", err)
	}
	return &encrypted, nil
}

func (s connectorTargetHandlers) canonicalCredentialPublic(ctx context.Context, runtime *databaseRuntime, connectorKind string, credentialKind string, public map[string]any) (map[string]any, error) {
	if adapter := s.connectorCredentialCanonicalizerFor(connectorKind); adapter != nil {
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
	case errors.Is(err, connectortargets.ErrTargetUpdateConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, connectortargets.ErrCredentialProfileUpdateConflict):
		writeError(w, http.StatusConflict, err.Error())
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
