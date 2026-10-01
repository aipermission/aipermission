package connectormanagement

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type cleanupEvidenceConnector struct {
	managementTestConnector
	read func(context.Context, connectors.CleanupEvidenceContext, connectors.CredentialProfileView) (*connectors.ActionResult, error)
}

func (cleanupEvidenceConnector) ProvisionedCredentialAdminProfileID(connectors.CredentialProfileView) (int64, bool, error) {
	return 17, true, nil
}

func (connector cleanupEvidenceConnector) ReadCompletedCredentialCleanup(ctx context.Context, evidence connectors.CleanupEvidenceContext, profile connectors.CredentialProfileView) (*connectors.ActionResult, error) {
	return connector.read(ctx, evidence, profile)
}

type cleanupEvidenceCapabilities struct{}

func (*cleanupEvidenceCapabilities) RuntimeCapability(string) connectors.RuntimeCapability {
	return nil
}

func cleanupEvidenceFixture(t *testing.T) (ManagedCredentialCleanupScope, connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	target := connectortargets.Target{ID: 4, ProjectID: 12, ConnectorKind: managementTestConnectorKind,
		Name: "My Target", Config: map[string]any{"endpoint": "current-endpoint"}, UpdatedAt: "current-target-revision"}
	profile := connectortargets.CredentialProfile{ID: 7, TargetID: 4, ConnectorKind: managementTestConnectorKind,
		Kind: "operator", Label: "managed", Public: map[string]any{"managed_marker": "generated"},
		EncryptedSecretJSON: "encrypted-profile", UpdatedAt: "current-profile-revision"}
	ports := managementCredentialRuntimePorts()
	ports.DecryptSecret = func(context.Context, int64, string) (map[string]any, error) {
		t.Fatal("local evidence read attempted credential decryption")
		return nil, nil
	}
	ports.RuntimeContext = func(connectortargets.Target, connectortargets.CredentialProfile, map[string]any, CredentialBoundary) connectors.RuntimeContext {
		t.Fatal("local evidence read attempted execution runtime creation")
		return connectors.RuntimeContext{}
	}
	return ManagedCredentialCleanupScope{Runtime: ports, EvidenceCapabilities: func(kind string) (connectors.RuntimeCapabilityResolver, error) {
		if kind != target.ConnectorKind {
			t.Fatalf("wrong resource scope: %q", kind)
		}
		return &cleanupEvidenceCapabilities{}, nil
	}}, target, profile
}
