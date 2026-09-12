package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
)

func TestWorkspaceHandlesRemainDistinctAndComponentScoped(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "second"}}

	first := component.handleFor(firstOwner)
	second := component.handleFor(secondOwner)
	if first == nil || second == nil || first == second {
		t.Fatal("independent workspace owners did not receive distinct capabilities")
	}
	if first.component == nil || first.component != second.component {
		t.Fatal("workspace capabilities did not retain their shared component identity")
	}
	workspace := component.WorkspaceOwner()
	if owner, ok := workspace.resolve(first); !ok || owner != firstOwner {
		t.Fatal("first workspace capability stopped resolving after adding the second")
	}
	if owner, ok := workspace.resolve(second); !ok || owner != secondOwner {
		t.Fatal("second workspace capability did not resolve to its owner")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if _, ok := foreign.WorkspaceOwner().resolve(first); ok {
		t.Fatal("workspace capability resolved in a foreign component")
	}
	component.forgetHandle(first)
	if _, ok := workspace.resolve(first); ok {
		t.Fatal("forgotten workspace capability remained valid")
	}
	if _, ok := component.AccessOwner().resolve(first); ok {
		t.Fatal("forgotten workspace capability remained valid through a feature owner")
	}
}

func TestOwnedWorkspaceSnapshotRetainsHandlesOutsideLifecycleRegistry(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	firstOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "first"}}
	secondOwner := &gatewayworkspace.Runtime{Identity: gatewayworkspace.RuntimeIdentity{DatabaseID: "second"}}
	first := component.handleFor(firstOwner)
	second := component.handleFor(secondOwner)

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
	first := component.handleFor(firstOwner)
	second := component.handleFor(secondOwner)

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
	if got, ok := workspace.LookupWorkspace("first"); !ok || got == first {
		t.Fatalf("forgotten handle was reused by lifecycle projection: (%p, %t)", got, ok)
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
	if _, err := workspace.AdoptWorkspace(t.Context(), gatewayworkspace.AdoptInput{}); !errors.Is(err, gatewayworkspace.InitializationError()) {
		t.Fatalf("nil workspace adoption error = %v, want initialization error", err)
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
	database := &sql.DB{}
	storageState := storage.New(database, nil, nil, "workspace", nil)
	owner := &gatewayworkspace.Runtime{Storage: &storageState}
	component := NewComponent(t.TempDir(), nil)
	handle := component.handleFor(owner)
	factory := &metadataReaderFactory{allowed: true}

	allowed, err := component.AccessOwner().CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Unix(123, 0))
	if err != nil || !allowed {
		t.Fatalf("allowed=%t err=%v", allowed, err)
	}
	if factory.database != database {
		t.Fatal("metadata reader did not receive the resolved workspace database")
	}

	foreign := NewComponent(t.TempDir(), nil)
	if allowed, err := foreign.AccessOwner().CanReadVaultMetadata(t.Context(), handle, factory, 7, 11, time.Now()); allowed || !errors.Is(err, gatewayaccess.ErrVaultMetadataAccessUnavailable) {
		t.Fatalf("foreign handle allowed=%t err=%v", allowed, err)
	}
}
