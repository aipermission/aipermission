package apiadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestSecretAccessorUsesSharedMissingSecretSentinel(t *testing.T) {
	_, err := (secretAccessor{}).GetSecret(context.Background(), "session_token")
	if !errors.Is(err, connectors.ErrSecretNotFound) {
		t.Fatalf("missing secret error = %v", err)
	}
}
