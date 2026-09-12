package gatewayconnectormanagement

import (
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func connectorTarget(target connectortargets.Target) connectorapi.Target {
	return connectorapi.Target{
		ID: target.ID, ProjectID: target.ProjectID, ProjectName: target.ProjectName, ProjectSlug: target.ProjectSlug,
		ConnectorKind: target.ConnectorKind, Name: target.Name, Config: target.Config,
		Status: string(target.Status), CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
	}
}
