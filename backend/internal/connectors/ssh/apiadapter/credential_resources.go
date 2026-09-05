package apiadapter

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (adapter) ListCredentialResources(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	items, err := keyStore.List(r.Context())
	if err != nil {
		writeInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (adapter) CreateCredentialResource(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	var input sshkeys.CreateRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Create(r.Context(), input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (adapter) ImportCredentialResource(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	var input sshkeys.ImportRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Import(r.Context(), input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (adapter) GetCredentialResource(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Get(r.Context(), id)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (adapter) UpdateCredentialResource(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	var input sshkeys.UpdateRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	item, err := keyStore.Update(r.Context(), id, input)
	if err != nil {
		handleKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (adapter) DeleteCredentialResource(w http.ResponseWriter, r *http.Request, runtime connectorapi.CredentialResourceRuntime) {
	if w == nil || r == nil {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		writeInternalError(w)
		return
	}
	if err := keyStore.Delete(r.Context(), id); err != nil {
		handleKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func connectorPayload(ctx context.Context, runtime connectorapi.CredentialResourceRuntime, name string, config map[string]any, profile map[string]any) (connectorPayloadValue, error) {
	if config == nil {
		config = map[string]any{}
	}
	if profile == nil {
		profile = map[string]any{}
	}
	targetConfig, err := targetConfigFromConnectorConfig(config)
	if err != nil {
		return connectorPayloadValue{}, err
	}
	sshConnector := sshconnector.New()
	if err := connectors.ValidateNonSecretSchema(sshConnector.TargetSchema(), "ssh target"); err != nil {
		return connectorPayloadValue{}, err
	}
	if err := connectors.ValidateSchemaValues(sshConnector.TargetSchema(), targetConfig); err != nil {
		return connectorPayloadValue{}, err
	}
	username := stringConfigValue(profile, "username")
	if username == "" {
		return connectorPayloadValue{}, connectortargets.ValidationError("username is required")
	}
	profilePublic, err := canonicalCredentialPublic(ctx, runtime, "private_key", map[string]any{
		"username":   username,
		"ssh_key_id": profile["ssh_key_id"],
	})
	if err != nil {
		return connectorPayloadValue{}, err
	}
	return connectorPayloadValue{
		Name:          strings.TrimSpace(name),
		TargetConfig:  targetConfig,
		ProfileLabel:  strings.TrimSpace(username),
		ProfilePublic: profilePublic,
	}, nil
}

func canonicalCredentialPublic(ctx context.Context, runtime connectorapi.CredentialResourceRuntime, credentialKind string, public map[string]any) (map[string]any, error) {
	if strings.TrimSpace(credentialKind) != "private_key" {
		return nil, connectortargets.ValidationError("unsupported SSH credential kind")
	}
	username := stringConfigValue(public, "username")
	if username == "" {
		return nil, connectortargets.ValidationError("username is required")
	}
	keyID := int64ConfigValue(public, "ssh_key_id")
	if keyID < 1 {
		return nil, connectortargets.ValidationError("ssh_key_id is required")
	}
	keyStore, err := keyStore(runtime)
	if err != nil {
		return nil, err
	}
	key, err := keyStore.Get(ctx, keyID)
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
