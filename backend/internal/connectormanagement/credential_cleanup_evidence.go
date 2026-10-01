package connectormanagement

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func completedCredentialCleanupEvidence(ctx context.Context, scope ManagedCredentialCleanupScope, connector connectors.Connector, target connectortargets.Target, profile connectortargets.CredentialProfile) (ProfileCleanupOutcome, bool, error) {
	result, handled, err := scope.Runtime.ReadCompletedCleanupEvidence(ctx, scope.EvidenceCapabilities, connector, target, profile)
	if err != nil || !handled {
		return ProfileCleanupOutcome{}, handled, err
	}
	return ProfileCleanupOutcome{Required: true, Status: string(result.Status), Output: result.Output}, true, nil
}

func completedCredentialCleanupOutcome(ctx context.Context, ports CredentialRuntimePorts, result connectors.ActionResult, cleanupErr error, boundary CredentialBoundary) (ProfileCleanupOutcome, error) {
	redacted, err := ports.ProjectCompletedCleanup(ctx, result, cleanupErr, boundary)
	if err != nil {
		return ProfileCleanupOutcome{}, err
	}
	return ProfileCleanupOutcome{Required: true, Status: string(redacted.Status), Output: redacted.Output}, nil
}
