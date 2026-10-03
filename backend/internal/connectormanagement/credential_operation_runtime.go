package connectormanagement

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

// PrepareCredentialOperationRuntime is core-only composition for one freshly
// loaded target/profile under lifecycle admission. The boundary must
// accompany the entire operation, including audit and response projection.
func PrepareCredentialOperationRuntime(ctx context.Context, ports CredentialRuntimePorts, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectors.RuntimeContext, CredentialBoundary, error) {
	runtime, boundary, _, err := prepareCredentialOperationRuntime(ctx, ports, target, profile)
	return runtime, boundary, err
}

// Provisioning retains the same secret map for bounded post-dispatch compensation.
func prepareCredentialOperationRuntime(ctx context.Context, ports CredentialRuntimePorts, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectors.RuntimeContext, CredentialBoundary, map[string]any, error) {
	if ctx == nil || !ports.Valid() || target.ID < 1 || profile.ID < 1 ||
		profile.TargetID != target.ID || target.ConnectorKind == "" || profile.ConnectorKind != target.ConnectorKind || profile.Kind == "" {
		return connectors.RuntimeContext{}, CredentialBoundary{}, nil, errors.New("credential operation runtime is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, nil, err
	}
	secrets, err := decryptCredentialSecrets(ctx, profile, ports.DecryptSecret)
	if err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, nil, err
	}
	boundary := actionresult.NewCredentialBoundary(secrets)
	runtime := ports.RuntimeContext(target, profile, secrets, boundary)
	if err := ctx.Err(); err != nil {
		return connectors.RuntimeContext{}, CredentialBoundary{}, nil, err
	}
	return runtime, boundary, secrets, nil
}
