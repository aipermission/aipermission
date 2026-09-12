package connectorports

import (
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func corePrincipal(principal connectorapi.Principal) (executionprincipal.Principal, error) {
	if err := principal.Validate(); err != nil {
		return executionprincipal.Principal{}, executionprincipal.ErrInvalid
	}
	return executionprincipal.Principal{
		Kind: executionprincipal.Kind(principal.Kind), TokenID: principal.TokenID,
		WorkspaceID: principal.WorkspaceID, RuntimeInstanceID: principal.RuntimeInstanceID,
	}, nil
}

func gatewayActionRequest(request connectortargets.ActionRequest) connectorapi.ActionRequest {
	return connectorapi.ActionRequest{
		ID: request.ID, ConnectorKind: request.ConnectorKind, ActionName: request.ActionName, Status: request.Status,
	}
}

func coreTarget(target connectorapi.Target) connectortargets.Target {
	return connectortargets.Target{
		ID: target.ID, ProjectID: target.ProjectID, ProjectName: target.ProjectName, ProjectSlug: target.ProjectSlug,
		ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
		Status: connectortargets.TargetStatus(target.Status), CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
	}
}
