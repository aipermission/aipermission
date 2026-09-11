package commandrequests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type commandWorkspaceFixture struct {
	state componentstate.State
}

func (workspace *commandWorkspaceFixture) ComponentStatePort() componentstate.Port {
	return &workspace.state
}

func TestWorkspaceComponentOwnsOneCommandRuntime(t *testing.T) {
	database, _ := commandRequestFixture(t)
	secretVault, err := vault.New("workspace-command-component-secret")
	if err != nil {
		t.Fatal(err)
	}
	workspace := &commandWorkspaceFixture{state: componentstate.New()}
	dependencies := WorkspaceRuntimeDependencies{
		Database: database, Vault: secretVault, WorkspaceID: "workspace-command-component",
		Redact:   func(_ context.Context, value string) string { return value },
		Sessions: &testActiveSessions{}, BackgroundTimeout: time.Second,
	}
	if err := InitializeWorkspace(workspace, dependencies); err != nil {
		t.Fatal(err)
	}
	first, err := RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeWorkspace(workspace, WorkspaceRuntimeDependencies{}); err != nil {
		t.Fatalf("idempotent initialization rebuilt the runtime: %v", err)
	}
	second, err := RuntimeForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("workspace returned different command runtime instances")
	}
}

func TestWorkspaceComponentFailsClosedWithoutStateOrInitialization(t *testing.T) {
	if err := InitializeWorkspace(nil, WorkspaceRuntimeDependencies{}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("nil workspace initialization error = %v", err)
	}
	if _, err := RuntimeForWorkspace(nil); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("nil workspace runtime error = %v", err)
	}
	workspace := &commandWorkspaceFixture{state: componentstate.New()}
	if _, err := RuntimeForWorkspace(workspace); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("uninitialized workspace runtime error = %v", err)
	}
}
