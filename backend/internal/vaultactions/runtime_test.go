package vaultactions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type testConnectorPort struct{}

func (testConnectorPort) SessionEnvironmentVersion(context.Context, int64) (string, error) {
	return "v1", nil
}
func (testConnectorPort) LiveConsolePermission(context.Context, int64, int64, int64, string) (connectortargets.ActionPermission, string, error) {
	return connectortargets.ActionPermission{}, "", errors.New("not used")
}
func (testConnectorPort) ExpectedPeerIdentities(context.Context, connectortargets.RuntimeSurface) (PeerIdentityExpectation, error) {
	return PeerIdentityExpectation{}, nil
}

type testDeliveryGate struct{}

func (testDeliveryGate) AcquireDelivery(context.Context) (func(), error)  { return func() {}, nil }
func (testDeliveryGate) AcquireExclusive(context.Context) (func(), error) { return func() {}, nil }

type testProjectPort struct{ store *projectstore.Store }

func (p testProjectPort) ResolveRef(ctx context.Context, ref string) (Project, bool, error) {
	project, err := p.store.ResolveRef(ctx, ref)
	if errors.Is(err, projectstore.ErrNotFound) {
		return Project{}, false, nil
	}
	return Project{ID: project.ID, Name: project.Name, Slug: project.Slug}, err == nil, err
}

func (p testProjectPort) TokenCanAccess(ctx context.Context, tokenID, projectID int64) (bool, error) {
	return p.store.TokenCanAccessProject(ctx, tokenID, projectID)
}

type testItemPort struct{ store *projectvault.Store }

func (p testItemPort) SnapshotSession(ctx context.Context, selections []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return p.store.SnapshotSession(ctx, selections)
}
func (p testItemPort) ResolveSession(ctx context.Context, selections []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return p.store.ResolveSession(ctx, selections)
}
func (p testItemPort) RevalidateSession(ctx context.Context, items []projectvault.SessionItem) error {
	return p.store.RevalidateSession(ctx, items)
}
func (p testItemPort) RecordSessionItems(ctx context.Context, sessionID int64, items []projectvault.SessionItem) error {
	return p.store.RecordSessionItems(ctx, sessionID, items)
}
func (p testItemPort) MarkSessionItemsUsed(ctx context.Context, items []projectvault.SessionItem) error {
	return p.store.MarkSessionItemsUsed(ctx, items)
}
func (p testItemPort) Create(ctx context.Context, tx *sql.Tx, input projectvault.CreateInput) (projectvault.Item, error) {
	return p.store.WithTx(tx).Create(ctx, input)
}
func (p testItemPort) Delete(ctx context.Context, id, valueVersion, metadataRevision int64) error {
	return p.store.Delete(ctx, id, valueVersion, metadataRevision)
}

type testSessions struct{}

func (testSessions) ActiveRecord(context.Context, int64) (console.Record, error) {
	return console.Record{}, console.ErrNotFound
}
func (testSessions) ReplaceIfCurrent(context.Context, executionprincipal.Principal, console.SessionHandle, console.CreateRequest) (console.Record, error) {
	return console.Record{}, errors.New("not used")
}
func (testSessions) Close(context.Context, executionprincipal.Principal, int64) error { return nil }

type testLeases struct{}

func (testLeases) Grant(vaultsessions.Lease) error     { return nil }
func (testLeases) RevokeSession(console.SessionHandle) {}
func (testLeases) Authorize(context.Context, executionprincipal.Principal, console.SessionAuthorization, console.SessionOperation) error {
	return nil
}

type testLeasePersistence struct{}

func (testLeasePersistence) Grant(context.Context, int64, vaultsessions.Lease) error { return nil }
func (testLeasePersistence) Revoke(context.Context, int64, int64) error              { return nil }

type testTokenReader struct{ store *tokens.Store }

func (reader testTokenReader) Get(ctx context.Context, id int64) (TokenState, error) {
	token, err := reader.store.Get(ctx, id)
	return TokenState{
		Active: token.ActiveAt(time.Now().UTC()), ExpiresAt: token.ExpiresAt, UpdatedAt: token.UpdatedAt,
	}, err
}

type runtimeFixture struct {
	runtime   *Runtime
	database  *sql.DB
	tokenID   int64
	projectID int64
	project   projectstore.Project
}

func newRuntimeFixture(t *testing.T, rule string) runtimeFixture {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "vault-actions.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projectstore.NewStore(database).Create(t.Context(), "Runtime Project")
	if err != nil {
		t.Fatal(err)
	}
	tokenStore := tokens.NewStore(database)
	token, err := tokenStore.Create(t.Context(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectstore.NewStore(database).ReplaceTokenScopes(t.Context(), token.ID, []int64{project.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := accesscontrol.NewCapabilityStore(database).Replace(t.Context(), token.ID, []accesscontrol.CapabilitySetInput{{
		ProjectID: project.ID, Name: accesscontrol.VaultItemGenerate, ExecutionRule: rule,
	}}); err != nil {
		t.Fatal(err)
	}
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatal(err)
	}
	itemStore := mustProjectVaultStore(t, database, secretVault)
	runtime, err := NewRuntime(Dependencies{
		Database: database, Tokens: testTokenReader{store: tokenStore},
		Projects:      testProjectPort{store: projectstore.NewStore(database)},
		SessionItems:  testItemPort{store: itemStore},
		ItemMutations: testItemPort{store: itemStore},
		Sessions:      testSessions{}, Leases: testLeases{}, PersistedLeases: testLeasePersistence{},
		Connector: testConnectorPort{}, Delivery: testDeliveryGate{},
		WorkspaceID: "workspace", RuntimeInstanceID: "runtime", MCPStarted: func() bool { return true },
		AllowGenerate: func(int64) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtimeFixture{runtime: runtime, database: database, tokenID: token.ID, projectID: project.ID, project: project}
}

func mustProjectVaultStore(t *testing.T, database *sql.DB, secretVault *vault.Vault) *projectvault.Store {
	t.Helper()
	store, err := projectvault.NewStore(database, secretVault, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPrepareSnapshotsGenerateAuthorizationAndExecutionRule(t *testing.T) {
	fixture := newRuntimeFixture(t, accesscontrol.RuleAlwaysRun)
	prepared, err := fixture.runtime.Prepare(
		t.Context(), fixture.tokenID, fixture.project.Slug, vaultrequests.ActionGenerateItem,
		map[string]any{
			"name": "PROJECT_TOKEN", "generator_kind": "hex_secret",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.RunImmediately || prepared.ProjectID != fixture.projectID || prepared.ApprovalContextHash == "" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if len(prepared.ApprovalContext.SourceProjectIDs) != 1 ||
		prepared.ApprovalContext.SourceProjectIDs[0] != fixture.projectID {
		t.Fatalf("source projects = %#v", prepared.ApprovalContext.SourceProjectIDs)
	}
	request := requestFromPrepared(t, fixture.tokenID, prepared)
	if err := fixture.runtime.ValidateAuthorization(t.Context(), request, prepared.ApprovalContext); err != nil {
		t.Fatalf("validate unchanged authorization: %v", err)
	}
	if _, err := accesscontrol.NewCapabilityStore(fixture.database).Replace(t.Context(), fixture.tokenID, []accesscontrol.CapabilitySetInput{{
		ProjectID: fixture.projectID, Name: accesscontrol.VaultItemGenerate,
		ExecutionRule: accesscontrol.RuleApprovalRequired,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.ValidateAuthorization(t.Context(), request, prepared.ApprovalContext); !IsStale(err) {
		t.Fatalf("changed capability error = %v", err)
	}
}

func TestPrepareValidatesGenerateMetadataBeforeApproval(t *testing.T) {
	fixture := newRuntimeFixture(t, accesscontrol.RuleApprovalRequired)
	valid, err := fixture.runtime.Prepare(
		t.Context(), fixture.tokenID, fixture.project.Slug, vaultrequests.ActionGenerateItem,
		map[string]any{"name": "PROJECT_TOKEN", "generator_kind": "hex_secret"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if valid.Input["secret_type"] != projectvault.DefaultSecretType {
		t.Fatalf("default secret type = %#v", valid.Input["secret_type"])
	}

	for name, input := range map[string]map[string]any{
		"secret type": {
			"name": "PROJECT_TOKEN", "secret_type": "certificate", "generator_kind": "hex_secret",
		},
		"generator": {
			"name": "PROJECT_TOKEN", "secret_type": "api_key", "generator_kind": "unknown",
		},
		"expiry": {
			"name": "PROJECT_TOKEN", "secret_type": "api_key", "generator_kind": "hex_secret", "expires_at": "not-rfc3339",
		},
		"warning days": {
			"name": "PROJECT_TOKEN", "secret_type": "api_key", "generator_kind": "hex_secret", "expiry_warning_days": 3651,
		},
		"owner repeated as shared": {
			"name": "PROJECT_TOKEN", "secret_type": "api_key", "generator_kind": "hex_secret",
			"shared_project_ids": []any{float64(fixture.projectID)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.runtime.Prepare(
				t.Context(), fixture.tokenID, fixture.project.Slug, vaultrequests.ActionGenerateItem, input,
			); err == nil {
				t.Fatal("invalid generation metadata reached approval")
			}
		})
	}
}

func TestPrepareRejectsHiddenAndUnknownProjects(t *testing.T) {
	fixture := newRuntimeFixture(t, accesscontrol.RuleApprovalRequired)
	if _, err := fixture.runtime.Prepare(t.Context(), fixture.tokenID, "missing", vaultrequests.ActionGenerateItem, map[string]any{
		"name": "PROJECT_TOKEN", "generator_kind": "hex_secret",
	}); !errors.Is(err, vaultrequests.ErrProjectNotFound) {
		t.Fatalf("unknown project error = %v", err)
	}
	if _, err := projectstore.NewStore(fixture.database).ReplaceTokenScopes(t.Context(), fixture.tokenID, nil); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.runtime.Prepare(t.Context(), fixture.tokenID, fixture.project.Slug, vaultrequests.ActionGenerateItem, map[string]any{
		"name": "PROJECT_TOKEN", "generator_kind": "hex_secret",
	})
	var preparation vaultrequests.PreparationError
	if !errors.As(err, &preparation) {
		t.Fatalf("hidden project error = %v", err)
	}
}

func TestExecuteGenerateRejectsTamperingAndCompensatesCreatedItem(t *testing.T) {
	fixture := newRuntimeFixture(t, accesscontrol.RuleAlwaysRun)
	prepared, err := fixture.runtime.Prepare(
		t.Context(), fixture.tokenID, fixture.project.Slug, vaultrequests.ActionGenerateItem,
		map[string]any{
			"name": "GENERATED_TOKEN", "secret_type": "generic_secret", "generator_kind": "hex_secret",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := requestFromPrepared(t, fixture.tokenID, prepared)
	tampered := request
	tampered.Input = map[string]any{
		"name": "OTHER_TOKEN", "secret_type": "generic_secret", "generator_kind": "hex_secret",
	}
	if _, err := fixture.runtime.Execute(t.Context(), tampered); !IsStale(err) {
		t.Fatalf("tampered input error = %v", err)
	}

	execution, handled, err := fixture.runtime.PrepareTransactional(t.Context(), request)
	if err != nil || !handled || execution.Run == nil || execution.Release == nil {
		t.Fatalf("prepare transactional generate handled=%v execution=%#v err=%v", handled, execution, err)
	}
	defer execution.Release()
	tx, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	output, observations, err := execution.Run(t.Context(), tx)
	if err != nil || len(observations) != 1 || observations[0].Action != "vault.item.created" {
		_ = tx.Rollback()
		t.Fatalf("transactional generate observations=%#v err=%v", observations, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	payload, ok := output.(map[string]any)
	if !ok || payload["secret_returned"] != false {
		t.Fatalf("generate output = %#v", output)
	}
	item, ok := payload["item"].(map[string]any)
	if !ok || jsonInt(item["item_id"]) < 1 {
		t.Fatalf("generated item output = %#v", payload["item"])
	}
	if !fixture.runtime.AuthorizeOutput(t.Context(), request) {
		t.Fatal("unchanged generate request output was not authorized")
	}
	if err := fixture.runtime.Compensate(t.Context(), request, output); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := fixture.database.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM vault_items WHERE id = ? AND status = 'active'`, jsonInt(item["item_id"]),
	).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("compensated item remains active: %d", active)
	}
}

func TestNormalizeIdentitiesDropsEmptyAndDuplicateValues(t *testing.T) {
	got := normalizeIdentities([]string{"peer-b", "", "peer-a", "peer-b"})
	want := []string{"peer-a", "peer-b"}
	if !equalStrings(got, want) {
		t.Fatalf("normalizeIdentities() = %#v, want %#v", got, want)
	}
}

func TestDeliveryAdmissionReleasesExactlyOnceForCallerAndPreparerOwnership(t *testing.T) {
	callerReleases := 0
	callerOwned := newDeliveryAdmission(func() { callerReleases++ })
	callerOwned.releaseIfUnclaimed()
	callerOwned.releaseIfUnclaimed()
	if callerReleases != 1 {
		t.Fatalf("unclaimed delivery releases = %d, want 1", callerReleases)
	}
	if _, err := callerOwned.claim(); err == nil {
		t.Fatal("released delivery admission was claimed")
	}

	preparerReleases := 0
	preparerOwned := newDeliveryAdmission(func() { preparerReleases++ })
	release, err := preparerOwned.claim()
	if err != nil {
		t.Fatal(err)
	}
	preparerOwned.releaseIfUnclaimed()
	if preparerReleases != 0 {
		t.Fatal("caller released a delivery admission after the preparer claimed it")
	}
	release()
	release()
	if preparerReleases != 1 {
		t.Fatalf("claimed delivery releases = %d, want 1", preparerReleases)
	}
}

func TestRuntimeRejectsIncompleteDependencies(t *testing.T) {
	if _, err := NewRuntime(Dependencies{}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	var runtime *Runtime
	if err := runtime.Compensate(t.Context(), vaultrequests.Request{}, nil); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("nil Runtime.Compensate() error = %v", err)
	}
}

func requestFromPrepared(t *testing.T, tokenID int64, prepared vaultrequests.PreparedAction) vaultrequests.Request {
	t.Helper()
	payload, err := json.Marshal(prepared.ApprovalContext)
	if err != nil {
		t.Fatal(err)
	}
	approval := map[string]any{}
	if err := json.Unmarshal(payload, &approval); err != nil {
		t.Fatal(err)
	}
	return vaultrequests.Request{
		TokenID: tokenID, ProjectID: prepared.ProjectID, ActionName: prepared.ApprovalContext.ActionName,
		Input: prepared.Input, ApprovalContext: approval, ApprovalContextHash: prepared.ApprovalContextHash,
	}
}
