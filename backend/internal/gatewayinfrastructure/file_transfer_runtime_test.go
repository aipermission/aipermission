package gatewayinfrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectorSecretAccessorIdentifiesMissingOptionalSecrets(t *testing.T) {
	_, err := (connectorSecretAccessor{values: map[string]any{}}).GetSecret(context.Background(), "password")
	if !errors.Is(err, connectors.ErrSecretNotFound) {
		t.Fatalf("missing secret error = %v, want ErrSecretNotFound", err)
	}
}
