package api

import (
	"context"

	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

func prepareConnectorAction(runtime databaseRuntime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	return actions.Prepare(runtime, ctx, request)
}
