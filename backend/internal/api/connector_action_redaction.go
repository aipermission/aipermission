package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (s *Server) redactedConnectorValueWithCredentialBoundary(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary connectorCredentialBoundary) (any, error) {
	return s.connectorActionApplication().RedactValue(ctx, s.connectorActionWorkspace(runtime), value, sensitiveFields, capabilityFields, boundary)
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return s.connectorActionApplication().RedactResult(ctx, s.connectorActionWorkspace(runtime), result, hints...)
}

func (s *Server) redactConnectorActionResultWithCredentialBoundary(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, result connectors.ActionResult, boundary connectorCredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return s.connectorActionApplication().RedactResultWithCredentialBoundary(ctx, s.connectorActionWorkspace(runtime), result, boundary, hints...)
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	return s.connectorActionApplication().RedactInput(ctx, s.connectorActionWorkspace(runtime), input, sensitiveInputFields)
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	return s.connectorActionApplication().RedactPreview(ctx, s.connectorActionWorkspace(runtime), preview, sensitiveFields, hints...)
}

func (s *Server) connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return s.connectorActionApplication().SensitiveOutputFields(hints...)
}
