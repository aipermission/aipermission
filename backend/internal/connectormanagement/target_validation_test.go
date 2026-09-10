package connectormanagement

import (
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type semanticTargetConnector struct{ managementTestConnector }

func (semanticTargetConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{{Name: "semantic_error", Type: connectors.FieldBoolean}}}
}

func (semanticTargetConnector) ValidateTargetConfig(config map[string]any) error {
	if value, _ := config["semantic_error"].(bool); value {
		return errors.New("semantic validation fixture")
	}
	return nil
}

func TestNormalizeTargetConfigRunsSchemaBeforeSemanticValidation(t *testing.T) {
	connector := semanticTargetConnector{}
	if _, err := NormalizeTargetConfig(connector, map[string]any{"unknown": true}); err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("schema validation error = %v", err)
	}
	if _, err := NormalizeTargetConfig(connector, map[string]any{"semantic_error": true}); err == nil || !strings.Contains(err.Error(), "semantic validation fixture") {
		t.Fatalf("semantic validation error = %v", err)
	}
}
