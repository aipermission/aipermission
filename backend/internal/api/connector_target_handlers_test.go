package api

import (
	"context"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestTransportConfigRejectsTargetsWithoutReviewedTCPAdapter(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().StoragePort().SecretVault(), fixture.server.activeRuntime().WorkspaceIdentifier())
	err := fixture.server.validateConnectorTransportConfig(context.Background(), store, target.ProjectID, map[string]any{
		"connection_mode":      "over_fixture",
		"transport_target_ref": connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
	})
	if err == nil || !strings.Contains(err.Error(), "does not expose reviewed TCP transport") {
		t.Fatalf("transport validation error = %v", err)
	}
}
