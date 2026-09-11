package filetransferhttp

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

type transferTestFixture struct {
	database  *sql.DB
	runtime   *transferapp.Runtime
	handlers  *Handlers
	runtimeID int64
	resolver  *testConnectorPortsResolver
	jobs      *transferjobs.Registry
	store     *filetransfer.Store
}

type testConnectorPortsResolver struct {
	resolve transferapp.ConnectorPortsResolver
}

func (r *testConnectorPortsResolver) Resolve(ctx context.Context, runtimeID int64) (transferapp.ConnectorPorts, error) {
	return r.resolve(ctx, runtimeID)
}

func (fixture transferTestFixture) execution(t *testing.T) transferExecution {
	t.Helper()
	execution, err := fixture.handlers.resolveTransferExecution(t.Context(), fixture.runtime, fixture.runtimeID)
	if err != nil {
		t.Fatalf("resolve transfer execution: %v", err)
	}
	return execution
}

func (fixture transferTestFixture) authorization(t *testing.T) connectorapi.TransferAuthorization {
	t.Helper()
	execution := fixture.execution(t)
	return connectorapi.TransferAuthorization{
		ConnectorKind:         execution.connectorKind,
		TargetID:              execution.target.ID,
		TargetRef:             execution.target.Ref,
		TargetUpdatedAt:       execution.target.UpdatedAt,
		ProfileID:             execution.profile.ID,
		ProfileUpdatedAt:      execution.profile.UpdatedAt,
		ProfileSecretRevision: execution.profile.SecretRevision,
	}
}

func newTransferTestFixture(t *testing.T) transferTestFixture {
	return newTransferTestFixtureWithAdapter(t, rejectingTransferAdapter{})
}

func newTransferTestFixtureWithAdapter(t *testing.T, transferAdapter connectorapi.FileTransferAdapter) transferTestFixture {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.aipdb"), "test-password")
	if err != nil {
		t.Fatalf("open transfer test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "transfer fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create transfer target: %v", err)
	}
	profile, err := targets.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "fixture", Label: "default", Public: map[string]any{},
	})
	if err != nil {
		t.Fatalf("create transfer profile: %v", err)
	}
	surface, err := targets.EnsureRuntimeSurface(t.Context(), connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: target.ConnectorKind, TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityFileTransfer, Label: "fixture transfer",
	})
	if err != nil {
		t.Fatalf("create transfer runtime surface: %v", err)
	}

	jobs := &transferjobs.Registry{}
	finalization := transferjobs.NewFinalizationLifetime()
	connectorScope := connectorruntime.NewScope(target.ConnectorKind, connectorruntime.Dependencies{
		Database: database,
		SecretAccessor: func(map[string]any) connectors.SecretAccessor {
			return testSecretAccessor{}
		},
	})
	resolver := &testConnectorPortsResolver{}
	resolver.resolve = func(context.Context, int64) (transferapp.ConnectorPorts, error) {
		return transferapp.ConnectorPorts{
			ConnectorKind: target.ConnectorKind, Gateway: testTransferGateway{},
			Runtime: connectorScope.TransferRuntime(), CredentialBoundary: actionresult.NewCredentialBoundary(nil),
		}, nil
	}
	runtime, err := transferapp.NewRuntime(transferapp.RuntimeDependencies{
		Database: database, Jobs: jobs, Finalization: finalization,
		Observe:        func(context.Context, string, *int64, int64, string, any) {},
		ConnectorPorts: resolver.Resolve,
	})
	if err != nil {
		t.Fatalf("create transfer runtime: %v", err)
	}
	handlers := NewHandlers(Dependencies{
		AdapterFor: func(kind string) connectorapi.FileTransferAdapter {
			if kind == target.ConnectorKind {
				return transferAdapter
			}
			return nil
		},
		DataPath: filepath.Join(t.TempDir(), "data", "test.aipdb"),
	})
	t.Cleanup(func() {
		jobs.Close()
		finalization.Stop()
	})
	return transferTestFixture{
		database: database, runtime: runtime, handlers: handlers, runtimeID: surface.ID,
		resolver: resolver, jobs: jobs, store: filetransfer.NewStore(database),
	}
}

type testSecretAccessor struct{}

func (testSecretAccessor) GetSecret(context.Context, string) (string, error) {
	return "", connectors.ErrSecretNotFound
}

type testTransferGateway struct{}

func (testTransferGateway) ConnectorTrustStorePath() string { return "" }
func (testTransferGateway) ConnectorRuntimeCapabilities() connectors.RuntimeCapabilityResolver {
	return nil
}

type rejectingTransferAdapter struct{}

func (rejectingTransferAdapter) BrowseRemoteFiles(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string) ([]connectorapi.RemoteFileEntry, error) {
	return nil, errors.New("unexpected connector browse")
}

func (rejectingTransferAdapter) StatRemotePath(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string) (connectorapi.RemotePathStatus, error) {
	return connectorapi.RemotePathStatus{Exists: true, Type: "file", Size: 1}, nil
}

func (rejectingTransferAdapter) UploadFile(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string, string, bool, connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	return connectorapi.TransferResult{}, errors.New("unexpected connector upload")
}

func (rejectingTransferAdapter) DownloadFile(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string, string, connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	return connectorapi.TransferResult{}, errors.New("unexpected connector download")
}
