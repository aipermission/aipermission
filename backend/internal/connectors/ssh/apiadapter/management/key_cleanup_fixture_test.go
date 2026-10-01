package management

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type cleanupResourceProbe struct {
	resourcecontract.CredentialResourceStore
	secretReads   int
	refuseSecret  bool
	createAfter   bool
	confirmAfter  bool
	confirmBefore bool
	onSecret      func()
}

func (store *cleanupResourceProbe) GetSecret(ctx context.Context, id int64, destination any) error {
	store.secretReads++
	if store.onSecret != nil {
		store.onSecret()
	}
	if store.refuseSecret {
		return errors.New("private key delivery was refused")
	}
	return store.CredentialResourceStore.GetSecret(ctx, id, destination)
}

func (store *cleanupResourceProbe) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	row, err := store.CredentialResourceStore.Create(ctx, input)
	if err == nil && store.createAfter {
		return resourcecontract.CredentialResource{}, errors.New("lost persisted intent response")
	}
	return row, err
}

func (store *cleanupResourceProbe) Update(ctx context.Context, id int64, input resourcecontract.UpdateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if store.confirmBefore {
		return resourcecontract.CredentialResource{}, errors.New("confirmation commit refused")
	}
	row, err := store.CredentialResourceStore.Update(ctx, id, input)
	if err == nil && store.confirmAfter {
		return resourcecontract.CredentialResource{}, errors.New("lost confirmation response")
	}
	return row, err
}

type cleanupRuntime struct {
	connectorapi.TargetLifecycleRuntime
	keys        *cleanupResourceProbe
	journal     *cleanupResourceProbe
	profiles    []connectors.CredentialProfileView
	profilesErr error
}

func (runtime *cleanupRuntime) CredentialResources(kind string) resourcecontract.CredentialResourceStore {
	if kind == "private_key" {
		return runtime.keys
	}
	if kind == keycleanup.ResourceKind {
		return runtime.journal
	}
	return nil
}

func (runtime *cleanupRuntime) ListCredentialProfiles(context.Context, int64) ([]connectors.CredentialProfileView, error) {
	return runtime.profiles, runtime.profilesErr
}

func (*cleanupRuntime) ListRuntimeSurfacesForProfile(context.Context, int64, int64, string) ([]connectorapi.RuntimeSurface, error) {
	return nil, nil
}

func (*cleanupRuntime) ConnectorLocalExecutionPrincipal() (connectorapi.Principal, error) {
	return connectorapi.Principal{}, nil
}

type cleanupGateway struct {
	path          string
	deleteCalls   int
	finalizeCalls int
	deleteErr     error
	finalizeErr   error
}

func (gateway *cleanupGateway) ConnectorTrustStorePath() string { return gateway.path }
func (*cleanupGateway) ConnectorRestartConsoleSession(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleRestartResult, error) {
	return connectorapi.ConsoleRestartResult{}, nil
}
func (gateway *cleanupGateway) ConnectorDeleteTargetRecord(context.Context, connectorapi.Target, map[string]any) error {
	gateway.deleteCalls++
	return gateway.deleteErr
}
func (gateway *cleanupGateway) ConnectorFinalizeDeletedTarget(context.Context, connectorapi.Target, string, map[string]any) (int64, error) {
	gateway.finalizeCalls++
	return 0, gateway.finalizeErr
}

type cleanupFixture struct {
	database *sql.DB
	path     string
	runtime  *cleanupRuntime
	gateway  *cleanupGateway
	target   connectorapi.Target
	key      sshkeys.SSHKey
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cleanup.db")
	database, err := dbpkg.OpenEncrypted(path, "cleanup-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("cleanup-fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	resources := connectorresources.NewStore(database, secretVault, "cleanup-fixture-workspace")
	runtime := &cleanupRuntime{
		keys:    &cleanupResourceProbe{CredentialResourceStore: resources.Scope("ssh", "private_key")},
		journal: &cleanupResourceProbe{CredentialResourceStore: resources.Scope("ssh", keycleanup.ResourceKind)},
	}
	key, err := sshkeys.NewResourceStore(runtime.keys).Create(t.Context(), sshkeys.CreateRequest{Name: "cleanup-test", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatal(err)
	}
	target := connectorapi.Target{ID: 1, ConnectorKind: "ssh", UpdatedAt: "target-v1", Config: map[string]any{"host": "127.0.0.1", "port": 65000}}
	runtime.profiles = []connectors.CredentialProfileView{{
		ID: 2, TargetID: target.ID, ConnectorKind: "ssh", UpdatedAt: "profile-v1", SecretRevision: "1",
		Public: map[string]any{"username": "operator", "ssh_key_id": key.ID},
	}}
	return &cleanupFixture{database: database, path: path, runtime: runtime, gateway: &cleanupGateway{path: filepath.Join(t.TempDir(), "known_hosts")}, target: target, key: key}
}

func (fixture *cleanupFixture) reopen(t *testing.T) {
	t.Helper()
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := dbpkg.OpenEncrypted(fixture.path, "cleanup-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("cleanup-fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	resources := connectorresources.NewStore(database, secretVault, "cleanup-fixture-workspace")
	fixture.database = database
	fixture.runtime.keys = &cleanupResourceProbe{CredentialResourceStore: resources.Scope("ssh", "private_key")}
	fixture.runtime.journal = &cleanupResourceProbe{CredentialResourceStore: resources.Scope("ssh", keycleanup.ResourceKind)}
}

func (fixture *cleanupFixture) delete(t *testing.T) (*httptest.ResponseRecorder, error) {
	t.Helper()
	return fixture.deleteContext(t.Context())
}

func (fixture *cleanupFixture) deleteContext(ctx context.Context) (*httptest.ResponseRecorder, error) {
	request := httptest.NewRequest("DELETE", "/api/connector-targets/1?remove_key=true", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	err := (Management{}).DeleteTarget(fixture.gateway, response, request, fixture.runtime, fixture.target)
	return response, err
}
