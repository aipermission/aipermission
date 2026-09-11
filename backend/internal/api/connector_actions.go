package api

import (
	"context"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

func prepareConnectorAction(runtime databaseRuntime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	workspace := actions.Workspace{}
	if runtime != nil {
		workspace.Storage.Database = runtime.StoragePort().DatabaseHandle()
		workspace.Storage.Registry = runtime.ConnectorPort().ConnectorRegistry()
	}
	return actions.Prepare(workspace, ctx, request)
}
