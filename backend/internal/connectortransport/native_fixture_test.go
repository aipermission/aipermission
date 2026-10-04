package connectortransport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type nativeTransportFixture struct {
	database *sql.DB
	store    *connectortargets.Store
	delivery *vaultsessions.DeliveryCoordinator
	runtime  Runtime
	project  int64
	source   string
	carrier  string
}

type nativeTransportScopes struct{ database *sql.DB }

func (scopes nativeTransportScopes) ConnectorScope(kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{Database: scopes.database, SecretAccessor: accessor})
}

func newNativeTransportFixture(t *testing.T) *nativeTransportFixture {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "transport.aipdb"), "DisposableTransportPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close native transport database: %v", err)
		}
	})
	project, err := projects.NewStore(database).Create(t.Context(), "Transport fixture")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &nativeTransportFixture{
		database: database, store: connectortargets.NewStore(database),
		delivery: &vaultsessions.DeliveryCoordinator{}, project: project.ID,
	}
	fixture.runtime = Runtime{
		Database: database, Scopes: nativeTransportScopes{database},
		AcquireDelivery: fixture.delivery.AcquireDelivery, Admission: fixture.delivery.AdmissionIdentity(),
	}
	fixture.source = fixture.addTarget(t, project.ID, "service", "Source")
	fixture.carrier = fixture.addTarget(t, project.ID, "carrier", "Carrier")
	return fixture
}

func (fixture *nativeTransportFixture) addTarget(t *testing.T, projectID int64, kind, name string) string {
	t.Helper()
	target, err := fixture.store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ProjectID: projectID, ConnectorKind: kind, Name: name, Config: map[string]any{"endpoint": "loopback"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := fixture.store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: kind, Kind: "operator", Label: "main",
		Public: map[string]any{"username": "operator"}, EncryptedSecretJSON: "opaque-fixture-ciphertext",
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := connectors.FormatTargetRef(kind, target.ID, profile.ID)
	if _, _, err := fixture.store.ResolveConnectorActionTarget(t.Context(), ref); err != nil {
		t.Fatalf("positive fixture resolution: %v", err)
	}
	return ref
}

func (fixture *nativeTransportFixture) requireQuiescent(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := fixture.delivery.AcquireExclusive(ctx)
	if err != nil {
		t.Fatalf("transport leaked delivery admission: %v", err)
	}
	release()
}

type commandAdapterFunc func(context.Context, connectorapi.PeerIdentityGateway, connectorapi.LiveConsoleRuntime, string, string) (connectors.CommandRunResult, error)

func (adapter commandAdapterFunc) RunConnectorCommand(ctx context.Context, peer connectorapi.PeerIdentityGateway, runtime connectorapi.LiveConsoleRuntime, targetRef, command string) (connectors.CommandRunResult, error) {
	return adapter(ctx, peer, runtime, targetRef, command)
}
