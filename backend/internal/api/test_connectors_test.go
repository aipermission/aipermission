package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

func testConnectorRegistry(t *testing.T) *connectors.Registry {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("new connector registry: %v", err)
	}
	return registry
}
