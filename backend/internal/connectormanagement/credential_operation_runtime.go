package connectormanagement

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// PrepareCredentialOperationRuntime is core-only composition for one freshly
// loaded target/profile under exclusive lifecycle admission. The boundary must
// accompany the entire operation, including audit and response projection.
func PrepareCredentialOperationRuntime(ctx context.Context, ports CredentialRuntimePorts, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectors.RuntimeContext, CredentialBoundary, error) {
	if ctx == nil || !ports.Valid() || target.ID < 1 || profile.ID < 1 ||
		profile.TargetID != target.ID || target.ConnectorKind == "" || profile.ConnectorKind != target.ConnectorKind || profile.Kind == "" {
		return connectors.RuntimeContext{}, CredentialBoundary{}, errors.New("credential operation runtime is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, err
	}
	secrets, err := decryptCredentialSecrets(ctx, profile, ports.DecryptSecret)
	if err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, err
	}
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, err
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	runtime := ports.RuntimeContext(target, profile, secrets, boundary)
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, err
	}
	return runtime, boundary, nil
}
