package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (s *Server) connectorActionRedactor(runtime *databaseRuntime) (*actions.Redactor, error) {
	return actions.NewRedactor(
		func(ctx context.Context, value string) string { return s.redactForPersistence(ctx, runtime, value) },
		func(ctx context.Context, value string) string { return s.redactCustom(ctx, runtime, value) },
		connectorActionJSONBodyBytes,
	)
}

func (s *Server) redactedConnectorValueWithCredentialBoundary(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary connectorCredentialBoundary) (any, error) {
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.ValueWithCredentialBoundary(ctx, value, sensitiveFields, capabilityFields, boundary)
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return redactor.Result(ctx, result, hints...)
}

func (s *Server) redactConnectorActionResultWithCredentialBoundary(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, boundary connectorCredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return redactor.ResultWithCredentialBoundary(ctx, result, boundary, hints...)
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *databaseRuntime, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.Input(ctx, input, sensitiveInputFields)
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *databaseRuntime, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	redactor, err := s.connectorActionRedactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.Preview(ctx, preview, sensitiveFields, hints...)
}

func connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	return actions.SensitiveOutputFields(hints...)
}
