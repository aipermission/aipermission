package api

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (s *Server) redactedConnectorValue(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool) (any, error) {
	return s.redactedConnectorValueWithLimits(ctx, runtime, value, sensitiveFields, capabilityFields, connectorCredentialBoundary{}, actionresult.DefaultLimits())
}

func (s *Server) redactedConnectorValueWithCredentialBoundary(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary connectorCredentialBoundary) (any, error) {
	return s.redactedConnectorValueWithLimits(ctx, runtime, value, sensitiveFields, capabilityFields, boundary, actionresult.DefaultLimits())
}

func (s *Server) redactedConnectorValueWithLimits(ctx context.Context, runtime *databaseRuntime, value any, sensitiveFields map[string]bool, capabilityFields map[string]bool, boundary connectorCredentialBoundary, sourceLimits actionresult.Limits) (any, error) {
	return actionresult.CanonicalizeAndRedactWithSourceLimits(value, sourceLimits, actionresult.DefaultLimits(), actionresult.RedactionOptions{
		SensitiveField: func(key string) bool {
			return connectorOutputFieldSensitive(key, sensitiveFields)
		},
		TemporaryCapabilityField: func(key string) bool {
			return connectorOutputFieldDeclared(key, capabilityFields)
		},
		RedactText: func(value string) string {
			return boundary.Redact(s.redactForPersistence(ctx, runtime, value))
		},
		RedactKey: func(value string) string {
			return boundary.RedactKey(s.redactForPersistence(ctx, runtime, value))
		},
		RedactCapability: func(value string) string {
			return boundary.Redact(s.redactCustom(ctx, runtime, value))
		},
	})
}

func (s *Server) redactConnectorActionResult(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, connectorCredentialBoundary{}, hints...)
}

func (s *Server) redactConnectorActionResultWithCredentialBoundary(ctx context.Context, runtime *databaseRuntime, result connectors.ActionResult, boundary connectorCredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	sensitiveFields := connectorSensitiveOutputFields(hints...)
	capabilityFields := connectorTemporaryCapabilityFields(hints...)
	result.DisplayText = boundary.Redact(s.redactForPersistence(ctx, runtime, result.DisplayText))
	result.Error = boundary.Redact(s.redactForPersistence(ctx, runtime, result.Error))
	redacted, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, result.Output, sensitiveFields, capabilityFields, boundary)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result.Output = redacted
	if result.Metadata != nil {
		redactedMetadata, err := s.redactedConnectorValueWithCredentialBoundary(ctx, runtime, result.Metadata, sensitiveFields, capabilityFields, boundary)
		if err != nil {
			return connectors.ActionResult{}, err
		}
		result.Metadata, _ = redactedMetadata.(map[string]any)
	}
	return result, nil
}

func (s *Server) redactConnectorActionInput(ctx context.Context, runtime *databaseRuntime, input map[string]any, sensitiveInputFields []string) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	fields := connectorSensitiveOutputFields()
	for _, field := range sensitiveInputFields {
		normalized := normalizeConnectorOutputField(field)
		if normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := s.redactedConnectorValueWithLimits(ctx, runtime, input, fields, nil, connectorCredentialBoundary{}, connectorActionInputRedactionLimits())
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, actionresult.ErrInvalidValue
	}
	return redacted, nil
}

func (s *Server) redactConnectorActionPreview(ctx context.Context, runtime *databaseRuntime, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	if preview == nil {
		return map[string]any{}, nil
	}
	fields := connectorSensitiveOutputFields(hints...)
	for _, field := range sensitiveFields {
		if normalized := normalizeConnectorOutputField(field); normalized != "" {
			fields[normalized] = true
		}
	}
	value, err := s.redactedConnectorValueWithLimits(ctx, runtime, preview, fields, nil, connectorCredentialBoundary{}, connectorActionInputRedactionLimits())
	if err != nil {
		return nil, err
	}
	redacted, ok := value.(map[string]any)
	if !ok || redacted == nil {
		return nil, actionresult.ErrInvalidValue
	}
	return redacted, nil
}

func connectorActionInputRedactionLimits() actionresult.Limits {
	limits := actionresult.DefaultLimits()
	limits.EncodedBytes = connectorActionJSONBodyBytes
	limits.StringBytes = connectorActionJSONBodyBytes
	return limits
}

func connectorTemporaryCapabilityFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{}
	for _, hint := range hints {
		for _, field := range hint.TemporaryCapabilityFields {
			if normalized := normalizeConnectorOutputField(field); normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func connectorSensitiveOutputFields(hints ...connectors.OutputHint) map[string]bool {
	fields := map[string]bool{
		"api_key":           true,
		"api_token_hash":    true,
		"apikey":            true,
		"authorization":     true,
		"credential":        true,
		"credential_hash":   true,
		"credential_value":  true,
		"password":          true,
		"password_hash":     true,
		"private_key":       true,
		"refresh_token":     true,
		"secret":            true,
		"secret_access_key": true,
		"secret_hash":       true,
		"secret_value":      true,
		"token":             true,
		"token_hash":        true,
	}
	for _, hint := range hints {
		for _, field := range hint.SensitiveFields {
			normalized := normalizeConnectorOutputField(field)
			if normalized != "" {
				fields[normalized] = true
			}
		}
	}
	return fields
}

func connectorOutputFieldSensitive(key string, sensitiveFields map[string]bool) bool {
	normalized := normalizeConnectorOutputField(key)
	if normalized == "" {
		return false
	}
	if sensitiveFields[normalized] {
		return true
	}
	for field := range sensitiveFields {
		if strings.HasSuffix(normalized, "."+field) || strings.HasSuffix(normalized, "_"+field) {
			return true
		}
	}
	return false
}

func connectorOutputFieldDeclared(key string, fields map[string]bool) bool {
	normalized := normalizeConnectorOutputField(key)
	return normalized != "" && fields[normalized]
}

func normalizeConnectorOutputField(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}
