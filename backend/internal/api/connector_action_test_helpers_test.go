package api

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const connectorActionTestWorkspaceID = "connector-action-test-workspace"

const connectorActionPersistenceUnknownMessage = actions.PersistenceUnknownMessage
const connectorActionLeaseExpiredBeforeDispatchMessage = actions.LeaseExpiredBeforeDispatchMessage

type connectorActionExecutionOptions = gatewayactions.ExecutionOptions
type connectorActionExecutionEnvelope = actions.ExecutionEnvelope

func prepareConnectorAction(runtime *gatewayinfra.WorkspaceHandle, ctx context.Context, request actions.PrepareRequest) (gatewayactions.PreparedRequest, error) {
	workspace := gatewayConnectorActionWorkspace(runtime)
	return gatewayConnectorActionComponent().Prepare(workspace, ctx, gatewayactions.PrepareRequest{
		Source: request.Source, TargetRef: request.TargetRef, ActionName: request.ActionName,
		Input: request.Input, Reason: request.Reason, CreatedAt: request.CreatedAt,
	})
}

func ownPreparedConnectorAction(prepared actions.PreparedRequest) gatewayactions.PreparedRequest {
	return gatewayactions.PreparedRequest{
		Target: prepared.Target, Profile: prepared.Profile, ConnectorVersion: prepared.ConnectorVersion,
		ActionDefinition: prepared.ActionDefinition, Action: prepared.Action,
		Requested: gatewayactions.PrepareRequest{
			Source: prepared.Requested.Source, TargetRef: prepared.Requested.TargetRef,
			ActionName: prepared.Requested.ActionName, Input: prepared.Requested.Input,
			Reason: prepared.Requested.Reason, CreatedAt: prepared.Requested.CreatedAt,
		},
		IdempotencyInput: prepared.IdempotencyInput, Dependencies: prepared.Dependencies,
	}
}

func gatewayConnectorActionComponent() *gatewayactions.Component {
	return gatewayactions.New(gatewayactions.Dependencies{})
}

func gatewayConnectorActionWorkspace(runtime *gatewayinfra.WorkspaceHandle) gatewayactions.Workspace {
	workspace := gatewayactions.Workspace{}
	if runtime != nil {
		owner, ok := runtimeTestOwners.Load(runtime)
		if ok {
			resources := owner.(runtimeTestOwner)
			workspace.Storage.Database = resources.database
			workspace.Storage.Registry = resources.registry
		}
	}
	return workspace
}

func (s *Server) insertConnectorActionRequest(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, tokenID int64, prepared gatewayactions.PreparedRequest, permission connectortargets.ActionPermission, status connectors.ResultStatus, errorText string, idempotencyKey string) (connectortargets.ActionRequest, bool, error) {
	persistence, err := s.connectorActions.Persistence(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	return persistence.InsertTokenRequest(ctx, tokenID, prepared, permission, status, errorText, idempotencyKey)
}

func (s *Server) insertPreparedConnectorActionRequest(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, tokenID *int64, prepared gatewayactions.PreparedRequest, status connectors.ResultStatus, errorText string, approvalContext string, approvalHash string, idempotencyKey string) (connectortargets.ActionRequest, bool, error) {
	persistence, err := s.connectorActions.Persistence(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	return persistence.InsertPreparedRequest(ctx, tokenID, prepared, status, errorText, approvalContext, approvalHash, idempotencyKey)
}

func (s *Server) executeInsertedConnectorAction(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, prepared gatewayactions.PreparedRequest, request connectortargets.ActionRequest, principal executionprincipal.Principal, options connectorActionExecutionOptions) (gatewayactions.CallResult, error) {
	dispatch, err := s.connectorActions.Dispatch(runtime)
	if err != nil {
		return gatewayactions.CallResult{}, err
	}
	return dispatch.ExecuteInserted(ctx, prepared, request, principal, options)
}

func (s *Server) snapshotPreparedConnectorAction(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, prepared gatewayactions.PreparedRequest) (gatewayactions.ExecutionSnapshot, error) {
	dispatch, err := s.connectorActions.Dispatch(runtime)
	if err != nil {
		return gatewayactions.ExecutionSnapshot{}, err
	}
	return dispatch.Snapshot(ctx, prepared)
}

func (s *Server) captureConnectorActionSessionHandleIfReturned(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, request connectortargets.ActionRequest, handles connectors.ActionHandles) (connectortargets.ActionRequest, error) {
	dispatch, err := s.connectorActions.Dispatch(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return dispatch.CaptureSessionHandleIfReturned(ctx, request, handles)
}

func connectorCredentialBoundaryForActionRequest(ctx context.Context, server *Server, runtime *gatewayinfra.WorkspaceHandle, requestID int64) (connectorCredentialBoundary, error) {
	recovery, err := server.connectorActions.Recovery(runtime)
	if err != nil {
		return connectorCredentialBoundary{}, err
	}
	return recovery.CredentialBoundaryForRequest(ctx, requestID)
}

func (s *Server) trackConnectorCredentialBoundary(runtime *gatewayinfra.WorkspaceHandle, requestID int64, boundary connectorCredentialBoundary) error {
	recovery, err := s.connectorActions.Recovery(runtime)
	if err != nil {
		return err
	}
	recovery.TrackCredentialBoundary(requestID, boundary)
	return nil
}

func (s *Server) connectorCredentialBoundary(runtime *gatewayinfra.WorkspaceHandle, requestID int64) (connectorCredentialBoundary, bool) {
	recovery, err := s.connectorActions.Recovery(runtime)
	if err != nil {
		return connectorCredentialBoundary{}, false
	}
	return recovery.CredentialBoundary(requestID)
}

func (s *Server) recoverOrphanedConnectorActions(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, now time.Time) {
	if recovery, err := s.connectorActions.Recovery(runtime); err == nil {
		recovery.Recover(ctx, now)
	}
}

func (s *Server) persistExpiredConnectorActionRecovery(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, requestID int64, now time.Time) (connectortargets.ActionRequest, error) {
	recovery, err := s.connectorActions.Recovery(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return recovery.PersistExpiredRecovery(ctx, requestID, now)
}

func (s *Server) beginConnectorActionDispatch(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, requestID int64) (connectortargets.ActionRequest, bool, error) {
	dispatch, err := s.connectorActions.Dispatch(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, false, err
	}
	return dispatch.BeginDispatch(ctx, requestID)
}

func connectorActionExecutionFailureStatus(err error) connectors.ResultStatus {
	return actions.ExecutionFailureStatus(err)
}

func connectorActionFailureOutput(err error) any { return actions.FailureOutput(err) }

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('workspace_uuid', ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, connectorActionTestWorkspaceID); err != nil {
		t.Fatalf("set test workspace identity: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func connectorActionTestRuntime(t *testing.T, database *sql.DB, secretVault *vault.Vault) *gatewayinfra.WorkspaceHandle {
	t.Helper()
	identityKey, err := actions.DeriveIdentityKey("test-password", connectorActionTestWorkspaceID)
	if err != nil {
		t.Fatalf("derive connector action identity key: %v", err)
	}
	runtime := newConnectorActionTestRuntime(
		t,
		database, secretVault, tokens.NewStore(database), testConnectorRegistry(t),
		connectorActionTestWorkspaceID, identityKey,
	)
	server := testServerForRuntime(t, runtime)
	testRuntimeControlState(t, server, runtime).SetMCPStarted(true)
	return runtime
}

func newTestDatabaseRuntime(t *testing.T, database *sql.DB) *gatewayinfra.WorkspaceHandle {
	t.Helper()
	secretVault, err := vault.New("test-password")
	if err != nil {
		t.Fatalf("create test Vault: %v", err)
	}
	return newConnectorActionTestRuntime(
		t, database, secretVault, tokens.NewStore(database), testConnectorRegistry(t),
		connectorActionTestWorkspaceID, connectorActionTestIdentityKey(t),
	)
}

func newConnectorActionTestRuntime(
	t *testing.T,
	database *sql.DB,
	secretVault *vault.Vault,
	tokenStore *tokens.Store,
	registry *connectors.Registry,
	workspaceUUID string,
	actionIdentityKey []byte,
) *gatewayinfra.WorkspaceHandle {
	t.Helper()
	infrastructure := gatewayinfra.NewComponent(filepath.Join(t.TempDir(), "workspace.aipdb"), nil)
	workspaceOwner := infrastructure.WorkspaceOwner()
	adapters := connectorapi.NewRegistry()
	adopted := testAdoptInput(database, secretVault, tokenStore)
	adopted.ID = workspaceUUID
	adopted.ConfiguredGatewaySecret = "test-password"
	adopted.Registry = registry
	adopted.AdapterRegistry = adapters
	adopted.RuntimeInstanceID = func() (string, error) { return "test-runtime", nil }
	runtime, err := workspaceOwner.AdoptWorkspace(t.Context(), adopted)
	if err != nil {
		t.Fatalf("adopt test runtime: %v", err)
	}
	registerRuntimeTestOwner(runtime, runtimeTestOwner{
		workspaceOwner: workspaceOwner, accessOwner: infrastructure.AccessOwner(),
		connectorActionOwner: infrastructure.ConnectorActionOwner(), connectorManagementOwner: infrastructure.ConnectorManagementOwner(),
		connectorPortsOwner: infrastructure.ConnectorPortsOwner(), observationOwner: infrastructure.ObservationOwner(),
		operationsOwner: infrastructure.OperationsOwner(), vaultOwner: infrastructure.VaultOwner(),
		database: database, secretVault: secretVault,
		tokens: tokenStore, registry: registry, adapters: adapters,
	})
	if runtime.Identity().WorkspaceID != workspaceUUID {
		t.Fatalf("workspace identifier = %q, want %q", runtime.Identity().WorkspaceID, workspaceUUID)
	}
	wantTag, err := actions.IdentityTag(actionIdentityKey, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	testServer := testServerForRuntime(t, runtime)
	gotTag, err := testServer.connectorActions.Tag(runtime, []byte("test"))
	if err != nil || gotTag != wantTag {
		t.Fatalf("test runtime action identity tag = %q, %v; want %q", gotTag, err, wantTag)
	}
	return runtime
}

func createSecurityPolicyRule(t testing.TB, ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, input securitypolicy.RuleInput) (securitypolicy.Rule, error) {
	t.Helper()
	server := testServerForRuntime(t, runtime)
	return server.accessOwner.CreateSecurityRule(ctx, runtime, input)
}

func setSecurityPolicySettings(t testing.TB, ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, settings securitypolicy.Settings) error {
	t.Helper()
	server := testServerForRuntime(t, runtime)
	_, err := server.accessOwner.UpdateSecuritySettings(ctx, runtime, settings)
	return err
}

func connectorActionTestIdentityKey(t *testing.T) []byte {
	t.Helper()
	key, err := actions.DeriveIdentityKey("test-password", connectorActionTestWorkspaceID)
	if err != nil {
		t.Fatalf("derive connector action identity key: %v", err)
	}
	return key
}

func openAPITestVault(t *testing.T) *vault.Vault {
	t.Helper()
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	return secretVault
}

func createAPITestPostgresTargetProfile(t *testing.T, store *connectortargets.Store, secretVault *vault.Vault, workspaceIDs ...string) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	workspaceID := connectorActionTestWorkspaceID
	if len(workspaceIDs) > 0 {
		workspaceID = workspaceIDs[0]
	}
	ctx := context.Background()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: postgresconnector.Kind,
		Name:          "main-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "disable",
		},
	})
	if err != nil {
		t.Fatalf("create postgres target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       postgresconnector.Kind,
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly"},
		EncryptedSecretJSON: "",
	})
	if err != nil {
		t.Fatalf("create postgres profile: %v", err)
	}
	encryptedSecret, err := recordcrypto.EncryptJSON(secretVault, workspaceID, recordcrypto.ConnectorCredentialProfile, profile.ID, map[string]any{"password": "secret"})
	if err != nil {
		t.Fatalf("encrypt profile secret: %v", err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(ctx, target.ID, profile.ID, encryptedSecret); err != nil {
		t.Fatalf("store profile secret: %v", err)
	}
	profile.EncryptedSecretJSON = encryptedSecret
	return target, profile
}

func insertAPITestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex', 'connector-hash', 'aip_conn', ?, ?)`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT ?, id, 1, ?, ? FROM projects WHERE status = 'active'`, id, now, now); err != nil {
		t.Fatalf("insert token project scopes: %v", err)
	}
	return id
}

const localActionTestConnectorKind = "localtest"

type localActionTestConnector struct{}

func (localActionTestConnector) ValidateTargetConfig(config map[string]any) error {
	if value, _ := config["semantic_error"].(bool); value {
		return fmt.Errorf("semantic validation fixture")
	}
	return nil
}

func (localActionTestConnector) Kind() string {
	return localActionTestConnectorKind
}

func (localActionTestConnector) Label() string {
	return "Local Test"
}

func (localActionTestConnector) Version() string {
	return "0.1"
}

func (localActionTestConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{}
}

func (localActionTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind:  "default",
		Label: "Default",
		Schema: connectors.Schema{Fields: []connectors.Field{{
			Name:   "password",
			Label:  "Password",
			Type:   connectors.FieldSecret,
			Secret: true,
		}}},
	}}
}

func (localActionTestConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{
		Title:       "Local test target",
		Summary:     "Test connector.",
		Connector:   "Local Test",
		ConnectorID: localActionTestConnectorKind,
	}, nil
}

func (localActionTestConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name:        "echo",
		Label:       "Echo",
		Description: "Echo one value.",
		Risk:        connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: []connectors.Field{{
			Name:     "value",
			Label:    "Value",
			Type:     connectors.FieldString,
			Required: true,
		}, {
			Name:    "mode",
			Label:   "Mode",
			Type:    connectors.FieldString,
			Default: "safe",
		}}},
	}}, nil
}

func (localActionTestConnector) PrepareAction(_ context.Context, req connectors.ActionRequest) (connectors.PreparedAction, error) {
	value, _ := req.Input["value"].(string)
	return connectors.PreparedAction{
		ConnectorKind: localActionTestConnectorKind,
		TargetRef:     req.Target.Ref,
		ProfileID:     req.Profile.ID,
		ActionName:    req.ActionName,
		Risk:          connectors.RiskRead,
		Title:         "Echo value",
		Summary:       "Echo one test value",
		Preview:       map[string]any{"value": value},
		Payload:       map[string]any{"value": value},
		ContextMaterial: map[string]any{
			"value": value,
		},
	}, nil
}

func (localActionTestConnector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if action.Payload["value"] == "classified-error" {
		return connectors.ActionResult{}, connectors.ClassifyError("fixture_failure", fmt.Errorf("fixture failed"))
	}
	if action.Payload["value"] == "reflect-credential" {
		secret, err := runtime.Secrets.GetSecret(ctx, "password")
		if err != nil {
			return connectors.ActionResult{}, err
		}
		return connectors.ActionResult{
			Status:      connectors.ResultCompleted,
			Output:      map[string]any{"echo": "permitted-target-output-may-be-sensitive-7f3a", "reflection": secret},
			DisplayText: "permitted-target-output-may-be-sensitive-7f3a " + secret,
			Metadata:    map[string]any{"reflection": secret},
		}, nil
	}
	result := connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"echo": action.Payload["value"]},
		DisplayText: fmt.Sprint(action.Payload["value"]),
	}
	if action.Payload["value"] == "with-handle" {
		result.Handles = connectors.ActionHandles{SessionID: 123, SessionGeneration: 456}
	}
	if action.Payload["value"] == "incomplete-handle" {
		result.Handles = connectors.ActionHandles{SessionID: 123}
	}
	return result, nil
}

func (localActionTestConnector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	secret, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil {
		return connectors.TestResult{}, err
	}
	return connectors.TestResult{
		Status:  connectors.TestOK,
		Message: "connection accepted " + secret,
		Details: map[string]any{"reflection": secret},
	}, nil
}
