package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func TestConnectorPeerTrustChangeInvalidatesEveryUnlockedWorkspace(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	first := fixture.server.activeRuntime()

	secondPath := filepath.Join(t.TempDir(), "second.aipdb")
	secondDB, err := dbpkg.OpenEncrypted(secondPath, "second-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := secondDB.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := fixture.server.openRuntime(secondPath, "second", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.mu.Lock()
	fixture.server.workspaces[second.id] = second
	fixture.server.mu.Unlock()

	firstRequest := createRuntimeScopedVaultRequest(t, first, "first")
	secondRequest := createRuntimeScopedVaultRequest(t, second, "second")
	changeCalled := false
	if err := fixture.server.ConnectorChangeVaultPeerTrust(ctx, func() error {
		changeCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !changeCalled {
		t.Fatal("trust change callback was not called")
	}
	for _, item := range []struct {
		runtime *databaseRuntime
		id      int64
	}{
		{runtime: first, id: firstRequest.ID},
		{runtime: second, id: secondRequest.ID},
	} {
		current, err := vaultrequests.NewStore(item.runtime.database).Get(ctx, item.id)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != vaultrequests.StatusStale {
			t.Fatalf("workspace %q request status = %q", item.runtime.id, current.Status)
		}
	}
}

func createRuntimeScopedVaultRequest(t *testing.T, runtime *databaseRuntime, suffix string) vaultrequests.Request {
	t.Helper()
	ctx := context.Background()
	project, err := projectstore.NewStore(runtime.database).Create(ctx, "Trust "+suffix)
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(runtime.database).Create(ctx, tokens.CreateRequest{Name: "trust-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(runtime.database)
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: "test", Name: "trust-" + suffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "trust-" + suffix,
		EncryptedSecretJSON: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := targets.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "test", TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeID := surface.ID
	request, _, err := vaultrequests.NewStore(runtime.database).Create(ctx, vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, RuntimeID: &runtimeID,
		ActionName:          vaultrequests.ActionRestartSession,
		Input:               map[string]any{"target_ref": "test:" + suffix},
		ApprovalContextHash: "trust-" + suffix, IdempotencyKey: "trust-" + suffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
