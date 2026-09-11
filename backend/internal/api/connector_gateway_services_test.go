package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func TestServerCloseCancelsRuntimeWorkAndClearsWorkspaces(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	transferCtx, cancelTransfer := context.WithCancel(context.Background())
	defer cancelTransfer()
	batchCtx, cancelBatch := context.WithCancel(context.Background())
	defer cancelBatch()
	jobs := requireTransferJobs(t, fixture.server, runtime)
	jobs.RegisterFileCancel(1, cancelTransfer)
	jobs.RegisterBatchCancel(1, cancelBatch)

	fixture.server.Close()

	select {
	case <-transferCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("server close did not cancel active runtime work")
	}
	if batchCtx.Err() == nil {
		t.Fatal("server close missed the batch sharing a file ID")
	}
	lateCtx, cancelLate := context.WithCancel(context.Background())
	defer cancelLate()
	jobs.RegisterFileCancel(2, cancelLate)
	if lateCtx.Err() == nil {
		t.Fatal("closed runtime accepted a late transfer")
	}
	if fixture.server.infrastructure.WorkspaceCount() != 0 || fixture.server.activeRuntime() != nil {
		t.Fatalf("server close retained unlocked runtime state: workspaces=%d", fixture.server.infrastructure.WorkspaceCount())
	}
}

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
	fixture.server.infrastructure.ActivateWorkspace(second)
	fixture.server.infrastructure.ActivateWorkspace(first)

	firstRequest := createRuntimeScopedVaultRequest(t, first, "first")
	secondRequest := createRuntimeScopedVaultRequest(t, second, "second")
	changeCalled := false
	if err := fixture.server.connectorPortsApplication().RouteGateway().ConnectorChangeVaultPeerTrust(ctx, func() error {
		changeCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !changeCalled {
		t.Fatal("trust change callback was not called")
	}
	for _, item := range []struct {
		runtime databaseRuntime
		id      int64
	}{
		{runtime: first, id: firstRequest.ID},
		{runtime: second, id: secondRequest.ID},
	} {
		current, err := vaultrequests.NewStore(item.runtime.Storage.DatabaseHandle()).Get(ctx, item.id)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != vaultrequests.StatusStale {
			t.Fatalf("workspace %q request status = %q", item.runtime.Identity.DatabaseID, current.Status)
		}
	}
}

func createRuntimeScopedVaultRequest(t *testing.T, runtime databaseRuntime, suffix string) vaultrequests.Request {
	t.Helper()
	ctx := context.Background()
	project, err := projectstore.NewStore(runtime.Storage.DatabaseHandle()).Create(ctx, "Trust "+suffix)
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(runtime.Storage.DatabaseHandle()).Create(ctx, tokens.CreateRequest{Name: "trust-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	targets := connectortargets.NewStore(runtime.Storage.DatabaseHandle())
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ProjectID: project.ID, ConnectorKind: "test", Name: "trust-" + suffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "trust-" + suffix,
		EncryptedSecretJSON: "",
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
	request, _, err := vaultrequests.NewStore(runtime.Storage.DatabaseHandle()).Create(ctx, vaultrequests.CreateInput{
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
