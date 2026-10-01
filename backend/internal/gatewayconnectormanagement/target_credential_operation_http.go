package gatewayconnectormanagement

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func runCredentialTargetOperation(w http.ResponseWriter, r *http.Request, workspace Workspace, gateway connectorapi.TargetOperationGateway, runner connectorapi.CredentialTargetOperationRunner, target Target, operation string, request map[string]any) {
	profileID, input, ok := credentialOperationInput(request)
	if !ok {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid credential operation request")
		return
	}
	profile, err := connectortargets.NewStore(workspace.Storage.Database).GetCredentialProfile(r.Context(), target.ID, profileID)
	if err != nil {
		WriteTargetError(w, err)
		return
	}
	if workspace.Storage.Registry == nil {
		httptransport.WriteInternalError(w)
		return
	}
	connector, found := workspace.Storage.Registry.Get(target.ConnectorKind)
	if !found {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported connector kind")
		return
	}
	if _, supported := connectormanagement.CredentialSchemaForKind(connector, profile.Kind); !supported {
		httptransport.WriteError(w, http.StatusBadRequest, "unsupported credential kind")
		return
	}
	runtime, boundary, err := connectormanagement.PrepareCredentialOperationRuntime(r.Context(), workspace.Credentials.OperationRuntime.domain(), target.domain(), profile)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	boundGateway := credentialOperationGateway{TargetOperationGateway: gateway, runtime: workspace.Credentials.OperationRuntime.domain(), boundary: boundary}
	response, err := runner.RunCredentialTargetOperation(r.Context(), boundGateway, runtime, operation, input)
	writeManagementResponseWithBoundary(w, r, workspace.Credentials.OperationRuntime, response, boundary, err)
}

func credentialOperationInput(request map[string]any) (int64, map[string]any, bool) {
	if len(request) != 2 {
		return 0, nil, false
	}
	text, ok := request["profile_id"].(string)
	if !ok {
		return 0, nil, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	input, inputOK := request["input"].(map[string]any)
	return id, input, err == nil && id > 0 && strconv.FormatInt(id, 10) == text && inputOK && input != nil
}

type credentialOperationGateway struct {
	connectorapi.TargetOperationGateway
	runtime  connectormanagement.CredentialRuntimePorts
	boundary connectormanagement.CredentialBoundary
}

func (gateway credentialOperationGateway) project(ctx context.Context, payload any) (any, error) {
	if gateway.runtime.RedactResult == nil || !gateway.boundary.Valid() {
		return nil, errors.New("credential operation audit projector is unavailable")
	}
	projected, err := gateway.runtime.RedactResult(ctx, connectors.ActionResult{Output: payload}, gateway.boundary)
	if err != nil {
		return nil, errors.New("credential operation audit projection failed")
	}
	return projected.Output, nil
}

func (gateway credentialOperationGateway) ConnectorWriteTargetAudit(ctx context.Context, action string, payload any) error {
	projected, err := gateway.project(ctx, payload)
	if err != nil {
		return err
	}
	return gateway.TargetOperationGateway.ConnectorWriteTargetAudit(ctx, action, projected)
}

func (gateway credentialOperationGateway) ConnectorWriteAudit(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	projected, err := gateway.project(ctx, payload)
	if err == nil {
		gateway.TargetOperationGateway.ConnectorWriteAudit(ctx, actor, tokenID, runtimeID, action, projected)
	}
}
