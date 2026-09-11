package api

import (
	"net/http"
	"strings"
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
	registry := runtimeConnectorRegistry(runtime)
	connector, ok := registry.Get(strings.TrimSpace(request.ConnectorKind))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := s.connectorManagementApplication().NormalizeTargetConfig(connector, request.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Config = config
	if err := s.validateConnectorTransportConfig(r.Context(), runtime, request.ProjectID, request.Config); err != nil {
		handleConnectorTargetError(w, err)
		return
	}
	if adapter := s.connectorDraftTesterFor(request.ConnectorKind); adapter != nil {
		adapter.TestDraft(s.connectorPortsApplication().PeerGateway(), w, r, s.connectorDataRuntimePort(runtime, request.ConnectorKind), request)
		return
	}
	writeError(w, http.StatusBadRequest, "draft test is not supported for this connector")
}
