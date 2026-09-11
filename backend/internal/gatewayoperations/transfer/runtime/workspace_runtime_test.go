package transferruntime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

type testWorkspace struct{ state componentstate.State }

func newTestWorkspace() *testWorkspace {
	return &testWorkspace{state: componentstate.New()}
}

func (workspace *testWorkspace) ComponentStatePort() componentstate.Port {
	if workspace == nil {
		return nil
	}
	return &workspace.state
}

func TestWorkspaceRuntimeOwnsOneTransferRuntime(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	workspace := newTestWorkspace()
	observe := func(context.Context, string, *int64, int64, string, any) {}
	resolve := func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil }

	if err := InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatal(err)
	}
	first, err := RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatal(err)
	}
	second, err := RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !WorkspaceReady(workspace) {
		t.Fatal("workspace transfer initialization was not idempotent")
	}
	StopWorkspace(workspace)
}

func TestWorkspaceRuntimeInitializationCanRetryAfterFailure(t *testing.T) {
	workspace := newTestWorkspace()
	observe := func(context.Context, string, *int64, int64, string, any) {}
	resolve := func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil }
	if err := InitializeWorkspace(workspace, nil, observe, resolve); err == nil {
		t.Fatal("missing database was accepted")
	}
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := InitializeWorkspace(workspace, database, observe, resolve); err != nil {
		t.Fatalf("retry initialization: %v", err)
	}
	StopWorkspace(workspace)
}

func TestWorkspaceShutdownCancelsRegisteredJobs(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	workspace := newTestWorkspace()
	if err := InitializeWorkspace(
		workspace,
		database,
		func(context.Context, string, *int64, int64, string, any) {},
		func(context.Context, int64) (ConnectorPorts, error) { return ConnectorPorts{}, nil },
	); err != nil {
		t.Fatal(err)
	}
	jobs, err := WorkspaceJobs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	jobContext, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs.RegisterFileCancel(42, cancel)
	initialized, drained, err := ShutdownWorkspace(workspace, time.Second, "interrupted", "queue stopped")
	if err != nil || !initialized || !drained {
		t.Fatalf("shutdown = initialized=%t drained=%t err=%v", initialized, drained, err)
	}
	if jobContext.Err() == nil {
		t.Fatal("shutdown did not cancel registered transfer")
	}
}

func TestUninitializedWorkspaceTransferLifecycleIsInert(t *testing.T) {
	workspace := newTestWorkspace()
	initialized, drained, err := ShutdownWorkspace(workspace, time.Millisecond, "interrupted", "queue stopped")
	if err != nil || initialized || !drained {
		t.Fatalf("shutdown = initialized=%t drained=%t err=%v", initialized, drained, err)
	}
	if !WaitWorkspace(t.Context(), workspace) {
		t.Fatal("empty workspace did not report drained")
	}
}
