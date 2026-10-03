package gatewayconnectormanagement

import (
	"net/http"
	"strings"

	targethttp "github.com/aipermission/aipermission/backend/internal/connectortargets/httpapi"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
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
		targethttp.WriteManagementError(w, err)
		return
	}
	adapter := handler.component.dependencies.Adapters.For(target.ConnectorKind)
	runner, _ := adapter.(connectorapi.TargetOperationRunner)
	operation := strings.TrimSpace(r.PathValue("operation"))
	credentialRunner, _ := adapter.(connectorapi.CredentialTargetOperationRunner)
	credentialOperation := !resourcecontract.IsNilDependency(credentialRunner) && credentialRunner.SupportsCredentialTargetOperation(operation)
	if resourcecontract.IsNilDependency(runner) && !credentialOperation {
		httptransport.WriteError(w, http.StatusBadRequest, "operation is not supported for this connector")
		return
	}
	if workspace.Adapters.OperationGateway == nil || (!credentialOperation && workspace.Adapters.DataRuntime == nil) {
		httptransport.WriteInternalError(w)
		return
	}
	input := map[string]any{}
	if !httptransport.DecodeJSON(w, r, &input, httptransport.DefaultJSONBodyBytes) {
		return
	}
	policy, _ := adapter.(connectorapi.TargetOperationLifecyclePolicy)
	exclusive := credentialOperation || (policy != nil && policy.RequiresTargetOperationExclusion(operation))
	release, ok := acquireTargetAdmission(w, r, workspace.Storage, exclusive, "connector target operation was canceled")
	if !ok {
		return
	}
	defer release()
	fresh, err := handler.component.Catalog(workspace.Storage.Database, workspace.Storage.Registry).Target(r.Context(), targetID)
	if err != nil {
		targethttp.WriteManagementError(w, err)
		return
	}
	if fresh.ConnectorKind != target.ConnectorKind {
		httptransport.WriteError(w, http.StatusConflict, "connector target changed; reload before retrying")
		return
	}
	target = fresh
	gateway := workspace.Adapters.OperationGateway(target.ConnectorKind, target.ID)
	if resourcecontract.IsNilDependency(gateway) {
		httptransport.WriteInternalError(w)
		return
	}
	if credentialOperation {
		runCredentialTargetOperation(w, r, workspace, gateway, credentialRunner, target, operation, input)
		return
	}
	runtime := workspace.Adapters.DataRuntime(target.ConnectorKind)
	if resourcecontract.IsNilDependency(runtime) {
		httptransport.WriteInternalError(w)
		return
	}
	response, err := runner.RunTargetOperation(
		r.Context(), gateway, runtime, connectorTarget(target), operation, input,
	)
	writeManagementResponse(w, r, workspace.Credentials.Runtime, response, err)
}
