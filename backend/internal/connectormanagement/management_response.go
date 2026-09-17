package connectormanagement

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// ProjectManagementResponse applies the same bounded credential and policy
// redaction used by connector actions before an adapter result reaches HTTP.
func ProjectManagementResponse(
	ctx context.Context,
	runtime CredentialRuntimePorts,
	response connectors.ManagementResponse,
	boundary CredentialBoundary,
) (int, any, error) {
	if runtime.RedactResult == nil {
		return 0, nil, fmt.Errorf("connector management response projector is unavailable")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode > 599 {
		return 0, nil, fmt.Errorf("connector management response has invalid status")
	}
	if !boundary.Valid() {
		boundary = actionresult.NewCredentialBoundary(nil)
	}
	boundary.Add(response.SensitiveValues...)
	projected, err := runtime.RedactResult(ctx, connectors.ActionResult{Output: response.Payload}, boundary)
	if err != nil {
		return 0, nil, fmt.Errorf("project connector management response: %w", err)
	}
	return response.StatusCode, projected.Output, nil
}
