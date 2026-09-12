package transferruntime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

type testWorkspace struct{ id string }

func newTestWorkspace() *testWorkspace { return &testWorkspace{id: "runtime-1"} }

func (workspace *testWorkspace) RuntimeIdentifier() string { return workspace.id }

func TestWorkspaceRuntimeOwnsOneTransferRuntime(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	workspace := newTestWorkspace()
	manager := &Manager{}
	observe := func(context.Context, string, *int64, int64, string, any) {}
	resolve := func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil }

	if err := manager.InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatal(err)
	}
	first, err := manager.RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatal(err)
	}
	second, err := manager.RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !manager.WorkspaceReady(workspace) {
		t.Fatal("workspace transfer initialization was not idempotent")
	}
	manager.StopWorkspace(workspace)
}

func TestWorkspaceRuntimeInitializationCanRetryAfterFailure(t *testing.T) {
	workspace := newTestWorkspace()
	manager := &Manager{}
	observe := func(context.Context, string, *int64, int64, string, any) {}
	resolve := func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil }
	if err := manager.InitializeWorkspace(workspace, nil, observe, resolve); err == nil {
		t.Fatal("missing database was accepted")
	}
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := manager.InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatalf("retry initialization: %v", err)
	}
	manager.StopWorkspace(workspace)
}

func TestWorkspaceShutdownCancelsRegisteredJobs(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	workspace := newTestWorkspace()
	manager := &Manager{}
	if err := manager.InitializeWorkspace(
		workspace,
		database,
		func(context.Context, string, *int64, int64, string, any) {},
		func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil },
	); err != nil {
		t.Fatal(err)
	}
	jobs, err := manager.WorkspaceJobs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	jobContext, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs.RegisterFileCancel(42, cancel)
	initialized, drained, err := manager.ShutdownWorkspace(workspace, time.Second, "interrupted", "queue stopped")
	if err != nil || !initialized || !drained {
		t.Fatalf("shutdown = initialized=%t drained=%t err=%v", initialized, drained, err)
	}
	if jobContext.Err() == nil {
		t.Fatal("shutdown did not cancel registered transfer")
	}
}

func TestWorkspaceRecoveryFailureRetainsRuntimeForRetry(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	workspace := newTestWorkspace()
	manager := &Manager{}
	if err := manager.InitializeWorkspace(
		workspace,
		database,
		func(context.Context, string, *int64, int64, string, any) {},
		func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil },
	); err != nil {
		t.Fatal(err)
	}
	initialized, err := manager.BeginWorkspaceShutdown(workspace)
	if err != nil || !initialized {
		t.Fatalf("begin shutdown = initialized=%t err=%v", initialized, err)
	}
	if !manager.WaitWorkspace(t.Context(), workspace) {
		t.Fatal("empty transfer runtime did not drain")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.RecoverWorkspace(t.Context(), workspace, "interrupted", "queue stopped"); err == nil {
		t.Fatal("closed storage did not fail transfer recovery")
	}
	if !manager.WorkspaceReady(workspace) {
		t.Fatal("failed transfer recovery removed the runtime needed for retry")
	}
	manager.RemoveWorkspace(workspace)
}

func TestUninitializedWorkspaceTransferLifecycleIsInert(t *testing.T) {
	workspace := newTestWorkspace()
	manager := &Manager{}
	initialized, drained, err := manager.ShutdownWorkspace(workspace, time.Millisecond, "interrupted", "queue stopped")
	if err != nil || initialized || !drained {
		t.Fatalf("shutdown = initialized=%t drained=%t err=%v", initialized, drained, err)
	}
	if !manager.WaitWorkspace(t.Context(), workspace) {
		t.Fatal("empty workspace did not report drained")
	}
}
