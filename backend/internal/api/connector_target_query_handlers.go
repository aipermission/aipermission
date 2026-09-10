package api

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s connectorTargetHandlers) testConnectorTargetDraft(w http.ResponseWriter, r *http.Request) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return
	}
	var request createConnectorTargetRequest
	if !decodeJSON(w, r, &request) {
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
	if err := s.validateConnectorTransportConfig(r.Context(), connectortargets.NewStore(runtime.database), request.ProjectID, request.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if adapter := s.connectorDraftTesterFor(request.ConnectorKind); adapter != nil {
		adapter.TestDraft(connectorPeerGatewayPort{server: s.Server}, w, r, connectorDataRuntimePort(runtime, request.ConnectorKind), request)
		return
	}
	writeError(w, http.StatusBadRequest, "draft test is not supported for this connector")
}
