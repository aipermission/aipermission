package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) redactedConnectorValueWithCredentialBoundary(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary gatewayactions.CredentialBoundary) (any, error) {
	return s.connectorActions.RedactValue(ctx, runtime, value, sensitiveFields, capabilityFields, boundary)
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return s.connectorActions.RedactResult(ctx, runtime, result, hints...)
}

func (s *Server) redactConnectorActionResultWithCredentialBoundary(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, result connectors.ActionResult, boundary gatewayactions.CredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return s.connectorActions.RedactResultWithCredentialBoundary(ctx, runtime, result, boundary, hints...)
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	return s.connectorActions.RedactInput(ctx, runtime, input, sensitiveInputFields)
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	return s.connectorActions.RedactPreview(ctx, runtime, preview, sensitiveFields, hints...)
}

func (s *Server) connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return s.connectorActions.SensitiveOutputFields(hints...)
}
