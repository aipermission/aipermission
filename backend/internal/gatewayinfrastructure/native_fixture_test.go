package gatewayinfrastructure

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
)

func openNativeInfrastructureHandle(t *testing.T, component *Component, id string) *WorkspaceHandle {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.aipdb")
	handle, err := component.WorkspaceOwner().OpenWorkspace(t.Context(), NewOpenWorkspaceInput(
		id, path, "DisposableInfrastructurePassword123", "fixture-gateway-"+id,
		connectors.NewRegistry(), connectorapi.NewRegistry(),
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		owner := component.WorkspaceOwner()
		if owner.valid(handle) {
			if err := owner.DiscardWorkspace(handle, nil, nil); err != nil {
				t.Errorf("discard native infrastructure: %v", err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := owner.WaitWorkspaceClosed(ctx, handle); err != nil {
			t.Errorf("join native infrastructure teardown: %v", err)
		}
	})
	return handle
}

func infrastructureOutboxCount(t *testing.T, handle *WorkspaceHandle) int {
	t.Helper()
	var count int
	if err := handle.workspace.WorkspaceDatabase().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func infrastructureDurableSettings(t *testing.T, handle *WorkspaceHandle) securitypolicy.Settings {
	t.Helper()
	settings, err := securitypolicy.NewService(handle.workspace.WorkspaceDatabase()).ReadSettings(t.Context())
	if err != nil {
		t.Fatalf("read uncached native settings: %v", err)
	}
	return settings
}
