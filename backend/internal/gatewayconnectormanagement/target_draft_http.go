package gatewayconnectormanagement

import (
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type TargetDraftHTTPHandler struct{ component *Component }

func (handler *TargetDraftHTTPHandler) Test(w http.ResponseWriter, r *http.Request) {
	if handler == nil || handler.component == nil {
		httptransport.WriteInternalError(w)
		return
	}
	workspace, ok := handler.component.active(w)
	if !ok {
		return
	}
	if workspace.Storage.Database == nil || workspace.Storage.Registry == nil {
		httptransport.WriteInternalError(w)
		return
	}
	var request connectormanagement.CreateTargetRequest
	if !httptransport.DecodeJSON(w, r, &request, httptransport.DefaultJSONBodyBytes) {
		return
	}
	request.ConnectorKind = strings.TrimSpace(request.ConnectorKind)
	connector, ok := workspace.Storage.Registry.Get(request.ConnectorKind)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	config, err := connectormanagement.NormalizeTargetConfig(connector, request.Config)
	if err != nil {
		httptransport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Config = config
	if err := handler.component.Catalog(workspace.Storage.Database, workspace.Storage.Registry).
		ValidateTargetTransport(r.Context(), request.ProjectID, request.Config); err != nil {
		WriteTargetError(w, err)
		return
	}
	adapter, _ := handler.component.dependencies.Adapters.For(request.ConnectorKind).(connectorapi.DraftTester)
	if adapter == nil {
		httptransport.WriteError(w, http.StatusBadRequest, "draft test is not supported for this connector")
		return
	}
	if handler.component.dependencies.PeerIdentity == nil || workspace.Adapters.DataRuntime == nil {
		httptransport.WriteInternalError(w)
		return
	}
	runtime := workspace.Adapters.DataRuntime(request.ConnectorKind)
	if runtime == nil {
		httptransport.WriteInternalError(w)
		return
	}
	adapter.TestDraft(handler.component.dependencies.PeerIdentity, w, r, runtime, request)
}
