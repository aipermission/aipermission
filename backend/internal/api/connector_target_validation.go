package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) validateConnectorTransportConfig(ctx context.Context, store *connectortargets.Store, projectID int64, config map[string]any) error {
	mode, _ := config["connection_mode"].(string)
	mode = strings.TrimSpace(mode)
	if !connectors.UsesConnectorTransport(mode, "direct") {
		return nil
	}
	transportTargetRef, _ := config["transport_target_ref"].(string)
	transportTargetRef = strings.TrimSpace(transportTargetRef)
	if transportTargetRef == "" {
		return connectortargets.ValidationError(fmt.Sprintf("transport target ref is required for connection mode %q", mode))
	}
	if err := store.ValidateTransportProject(ctx, projectID, transportTargetRef); err != nil {
		return err
	}
	kind, _, _, ok := connectors.ParseTargetRef(transportTargetRef)
	if !ok {
		return connectortargets.ErrInvalidTargetRef
	}
	if adapter, _ := s.connectorAPIAdapterFor(kind).(connectorapi.TCPTransportAdapter); adapter == nil {
		return connectortargets.ValidationError(fmt.Sprintf("%s connector does not expose reviewed TCP transport", kind))
	}
	return nil
}

type preparedConnectorCredentialProfileInput = connectormanagement.PreparedCredentialProfile
type connectorCredentialProfilePayload = connectormanagement.CredentialProfileInput

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
	prepared, err := connectormanagement.PrepareCredentialProfile(
		r.Context(), connector, request, secretRequired, previous, previousEncryptedSecret,
		s.connectorCredentialPreparationPorts(runtime),
	)
	if err == nil {
		return prepared, true
	}
	var inputErr connectormanagement.CredentialInputError
	switch {
	case errors.As(err, &inputErr):
		writeError(w, http.StatusBadRequest, inputErr.Error())
	case errors.Is(err, connectormanagement.ErrCredentialSecretDecode):
		writeInternalError(w)
	default:
		handleConnectorTargetError(w, err)
	}
	return preparedConnectorCredentialProfileInput{}, false
}

func (s *Server) encryptPreparedCredentialSecret(ctx context.Context, runtime *databaseRuntime, profileID int64, prepared preparedConnectorCredentialProfileInput) (*string, error) {
	return connectormanagement.EncryptPreparedCredentialSecret(
		ctx, profileID, prepared, s.connectorCredentialPreparationPorts(runtime),
	)
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
