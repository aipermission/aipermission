package gatewayconnectormanagement

import (
	"net/http"
	"strings"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type TargetOperationHTTPHandler struct{ component *Component }

func (handler *TargetOperationHTTPHandler) Run(w http.ResponseWriter, r *http.Request) {
	if handler == nil || handler.component == nil {
		httptransport.WriteInternalError(w)
		return
	}
	workspace, ok := handler.component.active(w)
	if !ok {
		return
	}
	targetID, ok := httptransport.ParsePathInt64(w, r, "id", "invalid id")
	if !ok {
		return
	}
	if workspace.Storage.Database == nil || handler.component.dependencies.Adapters == nil {
		httptransport.WriteInternalError(w)
		return
	}
	target, err := handler.component.Catalog(workspace.Storage.Database, workspace.Storage.Registry).Target(r.Context(), targetID)
	if err != nil {
		WriteTargetError(w, err)
		return
	}
	adapter, _ := handler.component.dependencies.Adapters.For(target.ConnectorKind).(connectorapi.TargetOperationRunner)
	if adapter == nil {
		httptransport.WriteError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	if workspace.Adapters.OperationGateway == nil || workspace.Adapters.DataRuntime == nil {
		httptransport.WriteInternalError(w)
		return
	}
	gateway := workspace.Adapters.OperationGateway(target.ConnectorKind, target.ID)
	runtime := workspace.Adapters.DataRuntime(target.ConnectorKind)
	if gateway == nil || runtime == nil {
		httptransport.WriteInternalError(w)
		return
	}
	adapter.RunTargetOperation(gateway, w, r, runtime, connectorTarget(target), strings.TrimSpace(r.PathValue("operation")))
}
