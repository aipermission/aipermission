package connectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type publicationCleanupConnector struct {
	managementTestConnector
	cleanupCount *int
}

func (connector publicationCleanupConnector) CleanupProvisionedCredentialProfile(context.Context, connectors.RuntimeContext, connectors.CredentialProfileView) (connectors.ActionResult, error) {
	*connector.cleanupCount++
	return connectors.ActionResult{Status: connectors.ResultCompleted}, nil
}
