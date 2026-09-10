package connectormanagement

import (
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func NormalizeTargetConfig(connector connectors.Connector, config map[string]any) (map[string]any, error) {
	if connector == nil {
		return nil, errors.New("connector is required")
	}
	if err := connectors.ValidateNonSecretSchema(connector.TargetSchema(), connector.Kind()+" target"); err != nil {
		return nil, err
	}
	normalized, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), config)
	if err != nil {
		return nil, err
	}
	if validator, ok := connector.(connectors.TargetConfigValidator); ok {
		if err := validator.ValidateTargetConfig(normalized); err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

func NormalizeTargetUpdate(connector connectors.Connector, existing, submitted map[string]any) (map[string]any, error) {
	if connector == nil {
		return nil, errors.New("connector is required")
	}
	if err := connectors.ValidateNonSecretSchema(connector.TargetSchema(), connector.Kind()+" target"); err != nil {
		return nil, err
	}
	if normalizer, ok := connector.(connectors.TargetConfigUpdateNormalizer); ok {
		submitted = normalizer.NormalizeTargetConfigUpdate(existing, submitted)
	}
	normalized, err := connectors.NormalizeSchemaValues(connector.TargetSchema(), submitted)
	if err != nil {
		return nil, err
	}
	if validator, ok := connector.(connectors.TargetConfigValidator); ok {
		if err := validator.ValidateTargetConfig(normalized); err != nil {
			return nil, err
		}
	}
	return normalized, nil
}
