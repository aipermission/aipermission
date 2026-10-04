package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
)

func nativeActionApplication(t *testing.T, component *Component) *ConnectorActionApplication {
	t.Helper()
	application, err := component.ConnectorActionOwner().NewConnectorActionApplication(64<<10, ConnectorActionPorts{
		Capabilities: func(*WorkspaceHandle, string, []connectors.ResolvedDependency, ConnectorActionFinishPort) connectors.RuntimeCapabilityResolver {
			return nil
		},
		SupportsRunning: func(gatewayactions.PreparedRequest) bool { return false },
		FinishRunning: func(context.Context, *WorkspaceHandle, int64, gatewayactions.PreparedRequest, gatewayaccess.Principal, connectors.ActionHandles, ConnectorActionFinishPort) {
			t.Error("boundary-only fixture unexpectedly executed running connector work")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func TestNativeInfrastructureActionCompositionRetainsOwnerAndRejectsStaleHandle(t *testing.T) {
	component := NewComponent(filepath.Join(t.TempDir(), "selection.aipdb"), nil)
	first := openNativeInfrastructureHandle(t, component, "first")
	second := openNativeInfrastructureHandle(t, component, "second")
	application := nativeActionApplication(t, component)
	firstWorkspace, firstOK := application.workspace(first)
	secondWorkspace, secondOK := application.workspace(second)
	if !firstOK || !secondOK || firstWorkspace.Storage.Database != first.workspace.WorkspaceDatabase() ||
		secondWorkspace.Storage.Database != second.workspace.WorkspaceDatabase() || firstWorkspace.Storage.Database == secondWorkspace.Storage.Database ||
		firstWorkspace.Storage.SecretVault == secondWorkspace.Storage.SecretVault || firstWorkspace.Storage.Tokens == secondWorkspace.Storage.Tokens {
		t.Fatal("actual action composition aliased workspace authority")
	}
	if firstWorkspace.Storage.WorkspaceID != first.Identity().WorkspaceID || secondWorkspace.Storage.WorkspaceID != second.Identity().WorkspaceID ||
		firstWorkspace.Identity.RuntimeInstanceID != first.Identity().RuntimeID || secondWorkspace.Identity.RuntimeInstanceID != second.Identity().RuntimeID {
		t.Fatal("action composition lost exact workspace/runtime identity")
	}
	if !component.AccessOwner().SetMCPStarted(first, true) || !component.AccessOwner().SetMCPStarted(second, false) ||
		!firstWorkspace.Identity.MCPStarted() || secondWorkspace.Identity.MCPStarted() {
		t.Fatal("captured action control callback crossed workspace")
	}
	canonical := []byte(`{"id":9007199254740993,"name":"\u03b1"}`)
	firstTag, err := application.Tag(first, canonical)
	if err != nil || firstTag == "" {
		t.Fatalf("first actual action tag: %q %v", firstTag, err)
	}
	secondTag, err := application.Tag(second, canonical)
	if err != nil || secondTag == "" || secondTag == firstTag {
		t.Fatalf("action tag crossed workspace identity: %q %v", secondTag, err)
	}
	beforeFirst, beforeSecond := infrastructureOutboxCount(t, first), infrastructureOutboxCount(t, second)
	mutations := 0
	payload := func() any { return map[string]any{"scope": "first"} }
	mutate := func(tx *sql.Tx) error {
		mutations++
		_, err := tx.ExecContext(t.Context(), `INSERT INTO settings(key, value, updated_at) VALUES('boundary-canary', 'first-only', datetime('now'))`)
		return err
	}
	err = firstWorkspace.Workflow.Mutate(t.Context(), "user", nil, 0, "boundary.canary", payload, mutate)
	if err != nil || mutations != 1 {
		t.Fatalf("matched live mutation control: %v calls=%d", err, mutations)
	}
	var canary string
	if err := first.workspace.WorkspaceDatabase().QueryRowContext(t.Context(), `SELECT value FROM settings WHERE key = 'boundary-canary'`).Scan(&canary); err != nil || canary != "first-only" {
		t.Fatalf("captured mutation did not bind first storage: %q %v", canary, err)
	}
	if err := second.workspace.WorkspaceDatabase().QueryRowContext(t.Context(), `SELECT value FROM settings WHERE key = 'boundary-canary'`).Scan(&canary); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("captured mutation crossed storage: %v", err)
	}
	if infrastructureOutboxCount(t, first) != beforeFirst+1 || infrastructureOutboxCount(t, second) != beforeSecond {
		t.Fatal("captured action observation was not bound to its storage")
	}
	foreign := nativeActionApplication(t, NewComponent(filepath.Join(t.TempDir(), "foreign.aipdb"), nil))
	if _, ok := foreign.workspace(first); ok {
		t.Fatal("foreign application accepted native handle")
	}
	if tag, err := foreign.Tag(first, canonical); tag != "" || !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("foreign application signed native action: %q %v", tag, err)
	}
	// Inject lost registry ownership while keeping native storage healthy. This
	// isolates handle rejection from closed-database or invalid-payload errors.
	t.Cleanup(func() {
		if err := component.workspace.Discard(first.workspace, nil, nil); err != nil {
			t.Errorf("discard forgotten fixture owner: %v", err)
		}
	})
	component.forgetHandle(first)
	if err := firstWorkspace.Storage.Database.PingContext(t.Context()); err != nil {
		t.Fatalf("registry fault also closed storage: %v", err)
	}
	if err := firstWorkspace.Identity.Ensure(); !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("captured identity admitted forgotten handle: %v", err)
	}
	if tag, err := application.Tag(first, canonical); tag != "" || !errors.Is(err, ErrWorkspaceHandleUnavailable) {
		t.Fatalf("forgotten handle signed action: %q %v", tag, err)
	}
	err = firstWorkspace.Workflow.Mutate(t.Context(), "user", nil, 0, "boundary.canary", payload, mutate)
	if err == nil || mutations != 1 {
		t.Fatalf("stale captured mutation reached callback: %v calls=%d", err, mutations)
	}
	if infrastructureOutboxCount(t, first) != beforeFirst+1 {
		t.Fatal("rejected captured mutation appended an audit event")
	}
	if err := secondWorkspace.Identity.Ensure(); err != nil || infrastructureOutboxCount(t, second) != beforeSecond {
		t.Fatalf("forgetting first disturbed second action authority: %v", err)
	}
}
