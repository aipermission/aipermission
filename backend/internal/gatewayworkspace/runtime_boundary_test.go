package gatewayworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/databaseownership"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

const boundaryPassword = "disposable-workspace-password"

func nativeWorkspaceInput(t *testing.T, id string) OpenInput {
	t.Helper()
	return OpenInput{
		ID: id, Path: filepath.Join(t.TempDir(), "workspace.aipdb"), Password: boundaryPassword,
		ConfiguredGatewaySecret: "disposable-workspace-gateway-secret",
		Registry:                connectorcatalog.NewRegistry(), AdapterRegistry: connectorapi.NewRegistry(),
	}
}

func openNativeWorkspace(t *testing.T, input OpenInput) *Runtime {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime, err := (&Component{}).Open(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := (&Component{}).Discard(runtime, nil, nil); err != nil {
			t.Errorf("discard native workspace: %v", err)
		}
	})
	return runtime
}

func TestNativeWorkspaceOpenPreservesIdentityAndExclusiveOwnership(t *testing.T) {
	input := nativeWorkspaceInput(t, "project-\u03b1")
	runtime := openNativeWorkspace(t, input)
	if !runtime.Identity.Ready() || runtime.Identity.DatabaseID != "project-\u03b1" ||
		runtime.Identity.DatabasePath != input.Path || runtime.Identity.UIRetryID == "" ||
		runtime.ConfiguredGatewaySecret() == "" {
		t.Fatalf("incomplete native identity: %#v", runtime.Identity)
	}
	file, err := os.Open(input.Path)
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 16)
	_, readErr := file.Read(header)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(header) == "SQLite format 3\x00" {
		t.Fatalf("native encrypted header: read=%v close=%v plaintext=%t", readErr, closeErr, string(header) == "SQLite format 3\x00")
	}
	if other, err := (&Component{}).Open(context.Background(), input); other != nil || !errors.Is(err, databaseownership.ErrDatabaseInUse) {
		t.Fatalf("simultaneous workspace open: runtime=%v err=%v", other != nil, err)
	}
	canonical := []byte(`{"action":"read","id":9007199254740993,"name":"\u03b1"}`)
	tag, err := runtime.TagActionIdentity(canonical)
	if err != nil || tag == "" {
		t.Fatalf("initial action tag: %q %v", tag, err)
	}
	identity, secret := runtime.Identity, runtime.ConfiguredGatewaySecret()
	database := runtime.WorkspaceDatabase()
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('boundary-canary', 'durable-canary', datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	completed := 0
	if err := (&Component{}).Close(runtime, nil, nil, nil, func() {
		if database.Ping() == nil {
			t.Error("completion ran before storage was closed")
		}
		completed++
	}); err != nil {
		t.Fatal(err)
	}
	if completed != 1 || database.Ping() == nil {
		t.Fatal("close did not release native storage before completion")
	}
	if _, err := runtime.TagActionIdentity(canonical); err == nil {
		t.Fatal("closed workspace still signs action identity")
	}
	wrong := input
	wrong.Password = "incorrect-fixture-password"
	if other, err := (&Component{}).Open(context.Background(), wrong); other != nil || !errors.Is(err, workspacelifecycle.ErrAuthentication) {
		t.Fatalf("wrong-password open: runtime=%v err=%v", other != nil, err)
	}
	input.ConfiguredGatewaySecret = "different-fallback-must-not-replace-persisted-secret"
	reopened := openNativeWorkspace(t, input)
	if reopened.Identity.WorkspaceID != identity.WorkspaceID || reopened.Identity.UIRetryID != identity.UIRetryID ||
		reopened.Identity.RuntimeID == identity.RuntimeID || reopened.ConfiguredGatewaySecret() != secret {
		t.Fatal("reopen lost persistent identity or reused runtime authority")
	}
	reopenedTag, err := reopened.TagActionIdentity(canonical)
	if err != nil || reopenedTag != tag {
		t.Fatalf("persistent replay identity changed: same=%t err=%v", reopenedTag == tag, err)
	}
	var canary string
	if err := reopened.WorkspaceDatabase().QueryRow(`SELECT value FROM settings WHERE key = 'boundary-canary'`).Scan(&canary); err != nil || canary != "durable-canary" {
		t.Fatalf("reopen lost durable canary: %q %v", canary, err)
	}
}

func TestNativeWorkspaceProjectionRemainsBoundToItsOwner(t *testing.T) {
	first := openNativeWorkspace(t, nativeWorkspaceInput(t, "first"))
	second := openNativeWorkspace(t, nativeWorkspaceInput(t, "second"))
	firstProjection, secondProjection := ProjectCapabilities(first), ProjectCapabilities(second)
	firstAccess, firstOK := firstProjection.Access.AccessControl.Current()
	secondAccess, secondOK := secondProjection.Access.AccessControl.Current()
	if !firstOK || !secondOK || firstAccess.Database != first.WorkspaceDatabase() || secondAccess.Database != second.WorkspaceDatabase() ||
		firstAccess.Database == secondAccess.Database || firstAccess.Tokens == secondAccess.Tokens || firstAccess.Policy == secondAccess.Policy || firstAccess.Delivery == secondAccess.Delivery {
		t.Fatal("workspace access authorities are aliased")
	}
	if _, ok := firstProjection.Access.ConsoleRecovery.Current(); ok {
		t.Fatal("console recovery appeared before configuration")
	}
	for index, projection := range []Projection{firstProjection, secondProjection} {
		configuration, ok := projection.Access.RuntimeConfiguration.Current()
		if !ok {
			t.Fatal("runtime configuration unavailable")
		}
		configuration.ConfigureConsole(nil, nil)
		if index == 0 {
			if _, ok := firstProjection.Access.ConsoleRecovery.Current(); !ok {
				t.Fatal("captured projection did not observe owner configuration")
			}
			if _, ok := secondProjection.Access.ConsoleRecovery.Current(); ok {
				t.Fatal("configuration published a different workspace's console")
			}
		}
	}
	firstSession, firstOK := firstProjection.Vault.Session.Current()
	secondSession, secondOK := secondProjection.Vault.Session.Current()
	if !firstOK || !secondOK || firstSession.Sessions == nil || secondSession.Sessions == nil ||
		firstSession.Sessions == secondSession.Sessions || firstSession.Leases == secondSession.Leases {
		t.Fatal("lazy console configuration crossed workspace boundaries")
	}
	firstCommand, commandOK := firstProjection.Operations.Command.Current()
	if !commandOK || firstCommand.Sessions != firstSession.Sessions {
		t.Fatal("command and Vault session projections lost shared owner")
	}
	firstControl, _ := firstProjection.Access.RuntimeControl.Current()
	secondControl, _ := secondProjection.Access.RuntimeControl.Current()
	firstControl.SetMCPStarted(true)
	firstObservation, _ := firstProjection.Observation.Runtime.Current()
	secondObservation, _ := secondProjection.Observation.Runtime.Current()
	if !firstObservation.MCPStarted() || secondObservation.MCPStarted() || secondControl.MCPStarted() {
		t.Fatal("runtime control change leaked to another workspace")
	}
	canonical := []byte("same request")
	firstTag, firstErr := firstProjection.ConnectorActions.Tag(canonical)
	secondTag, secondErr := secondProjection.ConnectorActions.Tag(canonical)
	if firstErr != nil || secondErr != nil || firstTag == secondTag {
		t.Fatal("distinct workspaces share replay identity")
	}
}

func TestNativeWorkspaceCanceledOpenReleasesStorageOwnership(t *testing.T) {
	input := nativeWorkspaceInput(t, "canceled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runtime, err := (&Component{}).Open(ctx, input)
	if runtime != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: runtime=%v err=%v", runtime != nil, err)
	}
	openNativeWorkspace(t, input)
}
