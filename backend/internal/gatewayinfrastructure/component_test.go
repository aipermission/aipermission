package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

func TestWorkspaceHandlesRemainDistinctAndComponentScoped(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "second"}}

	first := component.registerHandle(firstOwner)
	second := component.registerHandle(secondOwner)
	if first == nil || second == nil || first == second {
		t.Fatal("independent workspace owners did not receive distinct capabilities")
	}
	if first.component == nil || first.component != second.component {
		t.Fatal("workspace capabilities did not retain their shared component identity")
	}
	workspace := component.WorkspaceOwner()
	if owner, ok := workspace.lifecycleRuntime(first); !ok || owner != firstOwner {
		t.Fatal("first workspace capability stopped resolving after adding the second")
	}
	if owner, ok := workspace.lifecycleRuntime(second); !ok || owner != secondOwner {
		t.Fatal("second workspace capability did not resolve to its owner")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if _, ok := foreign.WorkspaceOwner().lifecycleRuntime(first); ok {
		t.Fatal("workspace capability resolved in a foreign component")
	}
	component.forgetHandle(first)
	if _, ok := workspace.lifecycleRuntime(first); ok {
		t.Fatal("forgotten workspace capability remained valid")
	}
	if component.AccessOwner().valid(first) {
		t.Fatal("forgotten workspace capability remained valid through a feature owner")
	}
}

func TestFeatureCapabilityRegistriesAreHandleScopedAndReleased(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	handle := component.registerHandle(&gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}})
	foreign := NewComponent(t.TempDir(), nil)

	checks := []struct {
		name      string
		available func(*Component, *WorkspaceHandle) bool
	}{
		{"access", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.AccessOwner().projection(handle)
			return ok
		}},
		{"connector actions", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.ConnectorActionOwner().projection(handle)
			return ok
		}},
		{"connector management", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.ConnectorManagementOwner().projection(handle)
			return ok
		}},
		{"connector ports", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.ConnectorPortsOwner().projection(handle)
			return ok
		}},
		{"observation", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.ObservationOwner().projection(handle)
			return ok
		}},
		{"operations", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.OperationsOwner().projection(handle)
			return ok
		}},
		{"vault", func(owner *Component, handle *WorkspaceHandle) bool {
			_, ok := owner.VaultOwner().projection(handle)
			return ok
		}},
	}
	for _, check := range checks {
		if !check.available(component, handle) {
			t.Fatalf("%s capabilities were not bound to the owned handle", check.name)
		}
		if check.available(foreign, handle) {
			t.Fatalf("%s capabilities crossed component ownership", check.name)
		}
	}

	component.forgetHandle(handle)
	for _, check := range checks {
		if check.available(component, handle) {
			t.Fatalf("%s capabilities remained available after workspace teardown", check.name)
		}
	}
}

func TestOwnedWorkspaceSnapshotRetainsHandlesOutsideLifecycleRegistry(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "second"}}
	first := component.registerHandle(firstOwner)
	second := component.registerHandle(secondOwner)

	snapshot := component.WorkspaceOwner().OwnedWorkspaceSnapshot()
	if len(snapshot) != 2 || !containsWorkspaceHandle(snapshot, first) || !containsWorkspaceHandle(snapshot, second) {
		t.Fatalf("owned snapshot = %#v, want both process-owned handles", snapshot)
	}
	component.forgetHandle(first)
	snapshot = component.WorkspaceOwner().OwnedWorkspaceSnapshot()
	if len(snapshot) != 1 || snapshot[0] != second {
		t.Fatalf("owned snapshot after teardown = %#v, want second handle only", snapshot)
	}
}

func TestWorkspaceOwnerProjectsOnlyOwnedLifecycleState(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	workspace := component.WorkspaceOwner()
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{
		DatabaseID: "first", DatabasePath: "/data/first.aipdb", UIRetryID: "retry-first",
	}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{
		DatabaseID: "second", DatabasePath: "/data/second.aipdb", UIRetryID: "retry-second",
	}}
	first := component.registerHandle(firstOwner)
	second := component.registerHandle(secondOwner)

	workspace.ActivateWorkspace(first)
	workspace.ActivateWorkspace(second)
	if !workspace.WorkspaceIsUnlocked() || workspace.WorkspaceCount() != 2 {
		t.Fatalf("workspace state unlocked=%t count=%d, want unlocked with two runtimes", workspace.WorkspaceIsUnlocked(), workspace.WorkspaceCount())
	}
	if got := workspace.ActiveWorkspace(); got != second {
		t.Fatalf("active workspace = %p, want second handle %p", got, second)
	}
	if got := workspace.WorkspaceSelection(); got.ID != "second" || got.Path != "/data/second.aipdb" || got.RetryIdentity != "retry-second" {
		t.Fatalf("selection = %#v, want second workspace identity", got)
	}
	if got, ok := workspace.LookupWorkspace("first"); !ok || got != first {
		t.Fatalf("lookup first = (%p, %t), want (%p, true)", got, ok, first)
	}
	if snapshot := workspace.WorkspaceSnapshot(); len(snapshot) != 2 || !containsWorkspaceHandle(snapshot, first) || !containsWorkspaceHandle(snapshot, second) {
		t.Fatalf("lifecycle snapshot = %#v, want both owned handles", snapshot)
	}

	foreign := NewComponent(t.TempDir(), nil)
	foreign.WorkspaceOwner().ActivateWorkspace(first)
	if foreign.WorkspaceOwner().WorkspaceIsUnlocked() || foreign.WorkspaceOwner().WorkspaceCount() != 0 {
		t.Fatal("foreign workspace owner accepted another component's capability")
	}
	component.forgetHandle(first)
	workspace.ActivateWorkspace(first)
	if got, ok := workspace.LookupWorkspace("first"); ok || got != nil {
		t.Fatalf("forgotten runtime was projected through a new handle: (%p, %t)", got, ok)
	}
}

func TestTransferWorkspaceUsesLocalDatabaseCopyIdentity(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	first := component.registerHandle(&gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{
		WorkspaceID: "shared-backup-workspace", RuntimeID: "runtime-first", UIRetryID: "copy-first",
	}})
	second := component.registerHandle(&gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{
		WorkspaceID: "shared-backup-workspace", RuntimeID: "runtime-second", UIRetryID: "copy-second",
	}})

	firstTransfer := component.OperationsOwner().TransferWorkspace(first)
	secondTransfer := component.OperationsOwner().TransferWorkspace(second)
	if firstTransfer.StorageID != "copy-first" || secondTransfer.StorageID != "copy-second" {
		t.Fatalf("transfer storage identities = (%q, %q), want database-copy identities", firstTransfer.StorageID, secondTransfer.StorageID)
	}
	if firstTransfer.StorageID == secondTransfer.StorageID {
		t.Fatal("database copies sharing a backup workspace UUID also shared transfer staging")
	}
}

func TestNilWorkspaceOwnerFailsClosed(t *testing.T) {
	var workspace *WorkspaceOwner
	if workspace.WorkspaceLifecycle() != nil || workspace.WorkspaceIsUnlocked() || workspace.WorkspaceCount() != 0 {
		t.Fatal("nil workspace owner exposed lifecycle state")
	}
	if workspace.WorkspaceSelection() != (Identity{}) || workspace.ActiveWorkspace() != nil || workspace.WorkspaceSnapshot() != nil || workspace.OwnedWorkspaceSnapshot() != nil {
		t.Fatal("nil workspace owner exposed workspace capabilities")
	}
	if handle, ok := workspace.LookupWorkspace("missing"); ok || handle != nil {
		t.Fatalf("nil workspace lookup = (%p, %t), want (nil, false)", handle, ok)
	}
	if _, err := workspace.OpenWorkspace(t.Context(), OpenWorkspaceInput{}); !errors.Is(err, gatewayworkspace.InitializationError()) {
		t.Fatalf("nil workspace open error = %v, want initialization error", err)
	}
}

func containsWorkspaceHandle(handles []*WorkspaceHandle, candidate *WorkspaceHandle) bool {
	for _, handle := range handles {
		if handle == candidate {
			return true
		}
	}
	return false
}

type metadataReaderFactory struct {
	database *sql.DB
	allowed  bool
	err      error
}

func (factory *metadataReaderFactory) ForDatabase(database *sql.DB) gatewayaccess.VaultMetadataReader {
	factory.database = database
	return factory
}

func (factory *metadataReaderFactory) CanRead(context.Context, int64, int64, time.Time) (bool, error) {
	return factory.allowed, factory.err
}

func TestVaultMetadataReadKeepsWorkspaceDatabaseInsideInfrastructure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.aipdb")
	database, err := dbpkg.OpenEncrypted(path, "TestPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	workspace := gatewayworkspace.NewComponent(t.TempDir(), nil)
	owner, err := workspace.Open(t.Context(), gatewayworkspace.OpenInput{
		ID: "workspace", Path: path, Password: "TestPassword123!", ConfiguredGatewaySecret: "gateway-secret",
		Registry: connectors.NewRegistry(), AdapterRegistry: connectorapi.NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Discard(owner, nil, nil) })
	component := NewComponent(t.TempDir(), nil)
	handle := component.registerHandle(owner)
	factory := &metadataReaderFactory{allowed: true}

	allowed, err := component.AccessOwner().CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Unix(123, 0))
	if err != nil || !allowed {
		t.Fatalf("allowed=%t err=%v", allowed, err)
	}
	if factory.database != owner.WorkspaceDatabase() {
		t.Fatal("metadata reader did not receive the resolved workspace database")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if allowed, err := foreign.AccessOwner().CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Now()); allowed || !errors.Is(err, gatewayaccess.ErrVaultMetadataAccessUnavailable) {
		t.Fatalf("foreign handle allowed=%t err=%v", allowed, err)
	}
}
