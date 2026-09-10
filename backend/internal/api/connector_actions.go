package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	applicationactions "github.com/aipermission/aipermission/backend/internal/applicationconnectoractions"
)

func prepareConnectorAction(runtime *databaseRuntime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	return applicationactions.Prepare(runtime, ctx, request)
}
