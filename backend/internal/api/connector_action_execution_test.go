package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type typedConnectorResultItem struct {
	Message  string            `json:"message"`
	Command  string            `json:"command"`
	Labels   map[string]string `json:"labels"`
	Password string            `json:"password"`
}

func TestCallConnectorActionBlocksMissingPermission(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultBlocked || result.Request.Status != connectors.ResultBlocked {
		t.Fatalf("expected blocked result/request, got %#v", result)
	}
	if result.Request.CompletedAt == nil || result.Request.Error == "" {
		t.Fatalf("blocked request should be terminal with error: %#v", result.Request)
	}
}

func TestCallConnectorActionCreatesPendingApproval(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1", "max_rows": 5},
		Reason:     "inspect one row",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultApprovalPending || result.Request.Status != connectors.ResultApprovalPending {
		t.Fatalf("expected pending result/request, got %#v", result)
	}
	if result.Request.EncryptedPayloadJSON == "" || result.Request.ApprovalContextHash == "" {
		t.Fatalf("pending request missing encrypted payload/context: %#v", result.Request)
	}
	if result.Result.Handles.RequestID != result.Request.ID || result.Result.Handles.FollowupTool == "" {
		t.Fatalf("pending result missing followup handle: %#v", result.Result)
	}
}

func TestRunPendingConnectorActionRejectsMissingApprovalIntegrity(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	executions := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(countingLocalActionTestConnector{executions: &executions}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	runtime := &databaseRuntime{
		database: database, vault: secretVault, tokens: tokens.NewStore(database),
		registry: registry, workspaceUUID: connectorActionTestWorkspaceID,
	}
	runtime.setMCPStarted(true)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind, Name: "integrity-target", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID: tokenID, TargetID: target.ID, ProfileID: profile.ID, ActionName: "echo",
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := server.callConnectorAction(t.Context(), runtime, connectorActionCall{
		Source: commandRequestSourceMCP, TokenID: tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo", Input: map[string]any{"value": "safe-preview"}, Reason: "verify integrity",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{
		"protect_connector_action_envelope_nonempty_update",
		"protect_connector_action_approval_hash_nonempty_update",
	} {
		if _, err := database.Exec(`DROP TRIGGER ` + trigger); err != nil {
			t.Fatalf("drop integrity trigger %s: %v", trigger, err)
		}
	}
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET encrypted_payload_json = '', approval_context_hash = '', input_json = '{"value":"tampered"}'
		WHERE id = ?`, pending.Request.ID); err != nil {
		t.Fatalf("corrupt pending request fixture: %v", err)
	}

	if _, err := server.runPendingConnectorAction(t.Context(), runtime, pending.Request.ID, ""); err == nil || !strings.Contains(err.Error(), "integrity data is missing") {
		t.Fatalf("run malformed pending request error = %v", err)
	}
	stale, err := store.GetActionRequest(t.Context(), pending.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != connectors.ResultStale || stale.ApprovalContextDrift != "request_integrity" || executions != 0 {
		t.Fatalf("malformed request was not rejected before dispatch: request=%#v executions=%d", stale, executions)
	}
}

func TestRunLocalConnectorActionCreatesManualHistory(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	registry := connectors.NewRegistry()
	if err := registry.Register(localActionTestConnector{}); err != nil {
		t.Fatalf("register local test connector: %v", err)
	}
	runtime := &databaseRuntime{
		database:      database,
		vault:         secretVault,
		tokens:        tokens.NewStore(database),
		registry:      registry,
		workspaceUUID: connectorActionTestWorkspaceID,
	}
	server := &Server{}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(context.Background(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind,
		Name:          "local-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(context.Background(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: localActionTestConnectorKind,
		Kind:          "default",
		Label:         "main",
		Public:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local profile: %v", err)
	}

	result, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "hello"},
		Reason:     "manual console smoke",
	})
	if err != nil {
		t.Fatalf("run local connector action: %v", err)
	}
	if result.Request.TokenID != nil || result.Request.Source != commandRequestSourceManual || result.Request.Status != connectors.ResultCompleted {
		t.Fatalf("unexpected local request: %#v", result.Request)
	}
	if result.Result.Output.(map[string]any)["echo"] != "hello" {
		t.Fatalf("unexpected output: %#v", result.Result.Output)
	}
	var historyCount int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM history_entries
		WHERE source_ref_type = ? AND source_ref_id = ? AND source = 'manual' AND connector_kind = ?`,
		historypkg.SourceConnectorActionRequest,
		result.Request.ID,
		localActionTestConnectorKind,
	).Scan(&historyCount); err != nil {
		t.Fatalf("read local action history: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("expected one local action history row, got %d", historyCount)
	}

	completedWithHandle, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "with-handle"},
		Reason:     "capture completed session handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completedWithHandle.Request.Status != connectors.ResultCompleted ||
		completedWithHandle.Request.SessionID == nil || *completedWithHandle.Request.SessionID != 123 ||
		completedWithHandle.Request.SessionGeneration == nil || *completedWithHandle.Request.SessionGeneration != 456 {
		t.Fatalf("completed action handle was not persisted: %#v", completedWithHandle.Request)
	}

	incompleteHandle, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "incomplete-handle"},
		Reason:     "reject incomplete session handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if incompleteHandle.Request.Status != connectors.ResultFailed ||
		incompleteHandle.Result.Status != connectors.ResultFailed ||
		!strings.Contains(incompleteHandle.Result.Error, "incomplete session handle") {
		t.Fatalf("incomplete session handle should fail terminally: %#v", incompleteHandle)
	}

	classifiedFailure, err := server.runLocalConnectorAction(context.Background(), runtime, connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "classified-error"},
		Reason:     "preserve a stable connector error code",
	})
	if err != nil {
		t.Fatal(err)
	}
	output, _ := classifiedFailure.Request.Output.(map[string]any)
	if classifiedFailure.Request.Status != connectors.ResultFailed || output["code"] != "fixture_failure" || classifiedFailure.Result.Output.(map[string]any)["code"] != "fixture_failure" {
		t.Fatalf("classified connector error code was not persisted: %#v", classifiedFailure)
	}
}

func TestRunLocalConnectorActionIdempotencyDoesNotExecuteTwice(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	executions := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(countingLocalActionTestConnector{executions: &executions}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	runtime := &databaseRuntime{database: database, vault: secretVault, tokens: tokens.NewStore(database), registry: registry, workspaceUUID: connectorActionTestWorkspaceID}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: localActionTestConnectorKind, Name: "idempotent-local", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	call := connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo", Input: map[string]any{"value": "once"}, Reason: "retry smoke",
		IdempotencyKey: "local-request-1",
	}
	first, err := (&Server{}).runLocalConnectorAction(t.Context(), runtime, call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (&Server{}).runLocalConnectorAction(t.Context(), runtime, call)
	if err != nil {
		t.Fatal(err)
	}
	if executions != 1 || first.Request.ID != second.Request.ID || !second.Replayed {
		t.Fatalf("executions=%d first=%d second=%d replayed=%v", executions, first.Request.ID, second.Request.ID, second.Replayed)
	}
	call.Input = map[string]any{"value": "once", "mode": "safe"}
	if _, err := (&Server{}).runLocalConnectorAction(t.Context(), runtime, call); !errors.Is(err, connectortargets.ErrActionRequestIdempotency) {
		t.Fatalf("explicit default must not replay a request that omitted it: %v", err)
	}
}

func TestExecuteInsertedConnectorActionDoesNotDispatchAfterRecoveryWins(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	executions := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(countingLocalActionTestConnector{executions: &executions}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	runtime := &databaseRuntime{
		database: database, vault: secretVault, tokens: tokens.NewStore(database),
		registry: registry, workspaceUUID: connectorActionTestWorkspaceID,
	}
	server := &Server{}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind, Name: "dispatch-race", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := runtime.prepareConnectorAction(t.Context(), actions.PrepareRequest{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "must-not-run"},
		Reason:     "dispatch ownership race",
		Source:     commandRequestSourceManual,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := localExecutionPrincipal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	request, created, err := server.insertPreparedConnectorActionRequest(
		t.Context(), runtime, nil, prepared, connectors.ResultRunning, "", "", "", "dispatch-race-1",
	)
	if err != nil || !created {
		t.Fatalf("insert running request: created=%v err=%v", created, err)
	}

	// This is the controlled barrier between durable insertion and dispatch:
	// execution is paused while recovery observes an expired ownership lease.
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET execution_lease_expires_at = ?
		WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), request.ID); err != nil {
		t.Fatal(err)
	}
	server.recoverOrphanedConnectorActions(t.Context(), runtime, time.Now().UTC())

	result, err := server.executeInsertedConnectorAction(
		t.Context(), runtime, prepared, request, principal, connectorActionExecutionOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if executions != 0 {
		t.Fatalf("connector executions=%d, want 0", executions)
	}
	if result.Request.Status != connectors.ResultOutcomeUnknown || result.Result.Status != connectors.ResultOutcomeUnknown {
		t.Fatalf("recovered request was not preserved: %#v", result)
	}
}

func TestBeginConnectorActionDispatchDoesNotTerminalizeActiveClaim(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	if err := ensureRuntimeIdentity(runtime); err != nil {
		t.Fatal(err)
	}
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	for _, test := range []struct {
		name              string
		owner             string
		dispatchStartedAt string
	}{
		{name: "another runtime owns the lease", owner: "another-runtime"},
		{name: "dispatch already started", owner: runtime.runtimeInstanceID, dispatchStartedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
				TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
				ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
				Status: connectors.ResultRunning, ExecutionOwner: test.owner,
				ExecutionLeaseExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano),
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.dispatchStartedAt != "" {
				if _, err := database.Exec(`UPDATE connector_action_requests SET dispatch_started_at = ? WHERE id = ?`, test.dispatchStartedAt, request.ID); err != nil {
					t.Fatal(err)
				}
			}

			if _, _, err := server.beginConnectorActionDispatch(t.Context(), runtime, request.ID); !errors.Is(err, connectortargets.ErrActionRequestExecutionClaim) {
				t.Fatalf("begin dispatch error = %v", err)
			}
			current, err := store.GetActionRequest(t.Context(), request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != connectors.ResultRunning || current.CompletedAt != nil {
				t.Fatalf("active claim was terminalized: %#v", current)
			}
		})
	}
}

func TestExecuteInsertedConnectorActionRejectsRevokedAlwaysPermissionBeforeDispatch(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	executions := 0
	registry := connectors.NewRegistry()
	if err := registry.Register(countingLocalActionTestConnector{executions: &executions}); err != nil {
		t.Fatal(err)
	}
	runtime := &databaseRuntime{
		database: database, vault: secretVault, tokens: tokens.NewStore(database),
		registry: registry, workspaceUUID: connectorActionTestWorkspaceID,
	}
	if err := ensureRuntimeIdentity(runtime); err != nil {
		t.Fatal(err)
	}
	runtime.setMCPStarted(true)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind, Name: "authorization-race", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	permissionInput := connectortargets.SetActionPermissionInput{
		TokenID: tokenID, TargetID: target.ID, ProfileID: profile.ID, ActionName: "echo",
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}
	if err := store.SetActionPermission(t.Context(), permissionInput); err != nil {
		t.Fatal(err)
	}
	prepared, err := runtime.prepareConnectorAction(t.Context(), actions.PrepareRequest{
		Source: commandRequestSourceMCP, TargetRef: connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo", Input: map[string]any{"value": "must-not-run"}, Reason: "authorization race", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	permission, err := store.GetActionPermission(t.Context(), tokenID, target.ID, profile.ID, "echo", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request, created, err := server.insertConnectorActionRequest(
		t.Context(), runtime, tokenID, prepared, permission, connectors.ResultRunning, "", "authorization-race-1",
	)
	if err != nil || !created {
		t.Fatalf("insert running request: created=%v err=%v", created, err)
	}
	permissionInput.ExecutionRule = connectortargets.ActionPermissionBlocked
	if err := store.SetActionPermission(t.Context(), permissionInput); err != nil {
		t.Fatal(err)
	}
	principal, err := tokenExecutionPrincipal(runtime, tokenID)
	if err != nil {
		t.Fatal(err)
	}

	result, err := server.executeInsertedConnectorAction(
		t.Context(), runtime, prepared, request, principal,
		connectorActionExecutionOptions{Permission: permission, RequiredPermissionRule: connectortargets.ActionPermissionAlwaysRun},
	)
	if err != nil {
		t.Fatal(err)
	}
	if executions != 0 {
		t.Fatalf("connector executions=%d, want 0", executions)
	}
	if result.Request.Status != connectors.ResultFailed || !strings.Contains(result.Request.Error, "authorization changed") {
		t.Fatalf("request did not fail closed: %#v", result.Request)
	}
}

type countingLocalActionTestConnector struct {
	localActionTestConnector
	executions    *int
	afterDispatch func()
}

func (c countingLocalActionTestConnector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	*c.executions++
	if c.afterDispatch != nil {
		c.afterDispatch()
	}
	return c.localActionTestConnector.ExecuteAction(ctx, runtime, action)
}

func TestRunLocalConnectorActionPreservesIdempotencyAfterTerminalPersistenceFailure(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	executions := 0
	registry := connectors.NewRegistry()
	connector := countingLocalActionTestConnector{
		executions: &executions,
		afterDispatch: func() {
			if _, err := database.Exec(`
				CREATE TRIGGER reject_connector_action_terminal_audit
				BEFORE INSERT ON audit_outbox
				WHEN NEW.action = 'connector_action.request.completed'
				BEGIN
					SELECT RAISE(FAIL, 'injected terminal audit failure');
				END`); err != nil {
				t.Fatalf("install terminal audit failure: %v", err)
			}
		},
	}
	if err := registry.Register(connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	runtime := &databaseRuntime{database: database, vault: secretVault, tokens: tokens.NewStore(database), registry: registry, workspaceUUID: connectorActionTestWorkspaceID}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: localActionTestConnectorKind, Name: "persistence-local", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	call := connectorActionCall{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo", Input: map[string]any{"value": "once"}, Reason: "persistence retry smoke",
		IdempotencyKey: "local-persistence-request-1",
	}
	_, err = (&Server{}).runLocalConnectorAction(t.Context(), runtime, call)
	var persistenceErr *connectorActionTerminalPersistenceError
	if !errors.As(err, &persistenceErr) || persistenceErr.RequestID < 1 {
		t.Fatalf("expected typed terminal persistence error, got %v", err)
	}
	response := httptest.NewRecorder()
	if !writeConnectorActionTerminalPersistenceError(response, err) || response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected persistence response: status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"outcome_unknown"`) || !strings.Contains(response.Body.String(), fmt.Sprintf(`"request_id":%d`, persistenceErr.RequestID)) {
		t.Fatalf("persistence response lacks request identity: %s", response.Body.String())
	}

	replayed, err := (&Server{}).runLocalConnectorAction(t.Context(), runtime, call)
	if err != nil {
		t.Fatalf("replay uncertain request: %v", err)
	}
	if executions != 1 || !replayed.Replayed || replayed.Request.ID != persistenceErr.RequestID {
		t.Fatalf("executions=%d replayed=%v request=%d want=%d", executions, replayed.Replayed, replayed.Request.ID, persistenceErr.RequestID)
	}
}

func TestConnectorActionExecutionSnapshotRejectsProfileDrift(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	registry := connectors.NewRegistry()
	if err := registry.Register(localActionTestConnector{}); err != nil {
		t.Fatalf("register local test connector: %v", err)
	}
	runtime := &databaseRuntime{database: database, vault: secretVault, registry: registry, workspaceUUID: connectorActionTestWorkspaceID}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(context.Background(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind,
		Name:          "local-target",
		Config:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(context.Background(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: localActionTestConnectorKind,
		Kind:          "default",
		Label:         "main",
		Public:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("create local profile: %v", err)
	}
	prepared, err := runtime.prepareConnectorAction(context.Background(), actions.PrepareRequest{
		TargetRef:  connectortargets.ConnectorTargetRef(localActionTestConnectorKind, target.ID, profile.ID),
		ActionName: "echo",
		Input:      map[string]any{"value": "hello"},
	})
	if err != nil {
		t.Fatalf("prepare action: %v", err)
	}
	if _, err := store.UpdateCredentialProfile(context.Background(), connectortargets.UpdateCredentialProfileInput{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: localActionTestConnectorKind,
		Kind:          "default",
		Label:         "changed",
		Public:        map[string]any{},
	}); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if _, err := (&Server{}).snapshotPreparedConnectorAction(context.Background(), runtime, prepared); err == nil || !strings.Contains(err.Error(), "changed after action preparation") {
		t.Fatalf("expected profile drift rejection, got %v", err)
	}
}

func TestInsertConnectorActionRequestRedactsDisplayedInputOnly(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	if _, err := insertRedactionRule(t.Context(), runtime, redactionRuleRequest{
		Name: "approval preview token", Pattern: `internal_[a-z0-9]+`, Enabled: true,
	}); err != nil {
		t.Fatalf("insert custom redaction rule: %v", err)
	}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	targetView, profileView, err := store.ResolveConnectorActionTarget(context.Background(), connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve target/profile: %v", err)
	}
	rawInput := map[string]any{
		"sql":            "select 'password=super-secret' as value",
		"access_token":   "raw-access-token",
		"opaque_message": "arbitrary-publish-content",
		"nested":         map[string]any{"authorization": "Bearer raw-bearer-token"},
	}
	prepared := actions.PreparedRequest{
		Target:  targetView,
		Profile: profileView,
		ActionDefinition: connectors.ActionDefinition{
			Name:                 postgresconnector.ActionQueryReadonly,
			SensitiveInputFields: []string{"access_token", "opaque_message", "client-secret"},
		},
		Action: connectors.PreparedAction{
			ConnectorKind: postgresconnector.Kind,
			TargetRef:     targetView.Ref,
			ProfileID:     profile.ID,
			ActionName:    postgresconnector.ActionQueryReadonly,
			Preview:       map[string]any{"body": "password=visible-for-approval internal_abc123", "opaque_message": "exact-sensitive-preview", "client_secret": "hyphen-normalized-preview-secret"},
			Payload:       rawInput,
		},
		Requested: actions.PrepareRequest{
			Source:     commandRequestSourceMCP,
			TargetRef:  targetView.Ref,
			ActionName: postgresconnector.ActionQueryReadonly,
			Input:      rawInput,
			Reason:     "Bearer raw-reason-token password=reason-secret",
		},
	}

	request, _, err := server.insertConnectorActionRequest(context.Background(), runtime, tokenID, prepared, connectortargets.ActionPermission{}, connectors.ResultApprovalPending, "", "")
	if err != nil {
		t.Fatalf("insert connector action request: %v", err)
	}
	var inputJSON string
	var previewJSON string
	var reason string
	var encryptedPayload string
	if err := database.QueryRow(`
			SELECT input_json, preview_json, reason, encrypted_payload_json
		FROM connector_action_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&inputJSON, &previewJSON, &reason, &encryptedPayload); err != nil {
		t.Fatalf("read connector action request: %v", err)
	}
	for _, secret := range []string{"super-secret", "raw-access-token", "arbitrary-publish-content", "raw-bearer-token", "raw-reason-token", "reason-secret"} {
		if strings.Contains(inputJSON, secret) || strings.Contains(reason, secret) {
			t.Fatalf("persisted connector request leaked %q: input=%s reason=%s", secret, inputJSON, reason)
		}
	}
	if !strings.Contains(inputJSON, `"access_token":"[REDACTED]"`) || !strings.Contains(inputJSON, `"opaque_message":"[REDACTED]"`) || !strings.Contains(inputJSON, `"authorization":"[REDACTED]"`) || !strings.Contains(inputJSON, `password=[REDACTED]`) {
		t.Fatalf("input was not redacted as expected: %s", inputJSON)
	}
	if strings.Contains(previewJSON, "visible-for-approval") || strings.Contains(previewJSON, "internal_abc123") {
		t.Fatalf("persisted display preview leaked exact approval content: %s", previewJSON)
	}
	if strings.Contains(previewJSON, "exact-sensitive-preview") || !strings.Contains(previewJSON, `"opaque_message":"[REDACTED]"`) {
		t.Fatalf("persisted display preview did not apply sensitive input fields: %s", previewJSON)
	}
	if strings.Contains(previewJSON, "hyphen-normalized-preview-secret") || !strings.Contains(previewJSON, `"client_secret":"[REDACTED]"`) {
		t.Fatalf("persisted display preview did not normalize declared sensitive fields: %s", previewJSON)
	}
	var historyInputJSON string
	if err := database.QueryRow(`
		SELECT input_json
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyInputJSON); err != nil {
		t.Fatalf("read connector history input: %v", err)
	}
	if historyInputJSON != inputJSON {
		t.Fatalf("history input drifted from redacted request input: history=%s request=%s", historyInputJSON, inputJSON)
	}
	mcpResponse := connectorActionRequestToMCPResponse(nil, request)
	approvalResponse := connectorActionApprovalItemFromRequest(request)
	if mcpResponse.Input["access_token"] != "[REDACTED]" || approvalResponse.Input["access_token"] != "[REDACTED]" {
		t.Fatalf("response input was not redacted: mcp=%#v approval=%#v", mcpResponse.Input, approvalResponse.Input)
	}
	if mcpResponse.Input["opaque_message"] != "[REDACTED]" || approvalResponse.Input["opaque_message"] != "[REDACTED]" {
		t.Fatalf("action-declared sensitive input was not redacted: mcp=%#v approval=%#v", mcpResponse.Input, approvalResponse.Input)
	}
	exactApproval, err := connectorActionApprovalItemForResponse(runtime, request)
	if err != nil {
		t.Fatalf("build exact approval response: %v", err)
	}
	if exactApproval.Preview["body"] != "password=visible-for-approval internal_abc123" {
		t.Fatalf("pending approval preview must match the exact prepared action: %#v", exactApproval.Preview)
	}
	if exactApproval.Preview["opaque_message"] != "exact-sensitive-preview" {
		t.Fatalf("exact approval preview must preserve sensitive content inside the encrypted envelope: %#v", exactApproval.Preview)
	}
	if exactApproval.Preview["client_secret"] != "hyphen-normalized-preview-secret" {
		t.Fatalf("exact approval preview must preserve normalized sensitive content inside the encrypted envelope: %#v", exactApproval.Preview)
	}
	redactedApproval := connectorActionApprovalItemFromRequest(request)
	if redactedApproval.Preview["body"] == "password=visible-for-approval internal_abc123" {
		t.Fatalf("approval list projection exposed exact preview: %#v", redactedApproval.Preview)
	}
	var decryptedPayload connectorActionExecutionEnvelope
	if err := recordcrypto.DecryptJSON(secretVault, runtime.workspaceUUID, recordcrypto.ConnectorActionRequest, request.ID, encryptedPayload, &decryptedPayload); err != nil {
		t.Fatalf("decrypt execution payload: %v", err)
	}
	if decryptedPayload.Input["access_token"] != "raw-access-token" || !strings.Contains(decryptedPayload.Input["sql"].(string), "super-secret") {
		t.Fatalf("encrypted execution payload should preserve raw input: %#v", decryptedPayload)
	}
	if decryptedPayload.Input["opaque_message"] != "arbitrary-publish-content" {
		t.Fatalf("encrypted execution payload should preserve action-sensitive input: %#v", decryptedPayload)
	}
	if decryptedPayload.Payload["access_token"] != "raw-access-token" || !strings.Contains(decryptedPayload.Payload["sql"].(string), "super-secret") {
		t.Fatalf("encrypted execution payload should preserve raw action payload: %#v", decryptedPayload)
	}
	if decryptedPayload.ApprovalPreview["body"] != "password=visible-for-approval internal_abc123" {
		t.Fatalf("encrypted approval preview changed: %#v", decryptedPayload.ApprovalPreview)
	}
	if !strings.Contains(decryptedPayload.Reason, "raw-reason-token") || !strings.Contains(decryptedPayload.Reason, "reason-secret") {
		t.Fatalf("encrypted execution payload should preserve raw reason: %#v", decryptedPayload)
	}
}

func TestRunningConnectorActionResponseRedactsOutput(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: postgresconnector.Kind,
		ActionName:    postgresconnector.ActionQueryReadonly,
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}
	redacted, err := server.redactConnectorActionResult(context.Background(), runtime, connectors.ActionResult{
		Status:      connectors.ResultRunning,
		Output:      map[string]any{"rows": []map[string]any{{"session_token": "raw-token", "name": "safe"}}},
		DisplayText: "Bearer raw-bearer-token",
		Error:       "password=super-secret",
	}, connectors.OutputHint{SensitiveFields: []string{"session_token"}})
	if err != nil {
		t.Fatal(err)
	}
	response := connectorActionToMCPResponse(nil, request, redacted)
	payload := fmt.Sprint(response.Output, response.DisplayText, response.Error)
	for _, secret := range []string{"raw-token", "raw-bearer-token", "super-secret"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("running response leaked %q: %#v", secret, response)
		}
	}
}

func TestFinishConnectorActionRequestCanonicalizesTypedOutputBeforePersistence(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	if _, err := insertRedactionRule(t.Context(), runtime, redactionRuleRequest{
		Name: "typed output token", Pattern: `internal_[a-z0-9]+`, Enabled: true,
	}); err != nil {
		t.Fatalf("insert custom redaction rule: %v", err)
	}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
		Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert action request: %v", err)
	}

	finished, err := server.finishConnectorActionRequest(
		t.Context(), runtime, request.ID, connectors.ResultCompleted,
		map[string]any{"events": []typedConnectorResultItem{{
			Message:  "Bearer raw-bearer-token internal_abc123",
			Command:  "password=command-secret",
			Labels:   map[string]string{"authorization": "Bearer label-token", "safe": "visible"},
			Password: "field-secret",
		}}},
		"done", "",
	)
	if err != nil {
		t.Fatalf("finish typed action output: %v", err)
	}

	encoded := fmt.Sprint(finished.Output)
	for _, secret := range []string{"raw-bearer-token", "internal_abc123", "command-secret", "label-token", "field-secret"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("finished output leaked %q: %s", secret, encoded)
		}
	}
	if !strings.Contains(encoded, "visible") || !strings.Contains(encoded, "[REDACTED]") {
		t.Fatalf("finished output lost safe data or markers: %s", encoded)
	}

	var requestOutput, historyOutput string
	if err := database.QueryRow(`SELECT output_json FROM connector_action_requests WHERE id = ?`, request.ID).Scan(&requestOutput); err != nil {
		t.Fatalf("read request output: %v", err)
	}
	if err := database.QueryRow(`SELECT output_json FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, request.ID).Scan(&historyOutput); err != nil {
		t.Fatalf("read history output: %v", err)
	}
	if requestOutput != historyOutput {
		t.Fatalf("history projection drifted from request: request=%s history=%s", requestOutput, historyOutput)
	}
	for _, secret := range []string{"raw-bearer-token", "internal_abc123", "command-secret", "label-token", "field-secret"} {
		if strings.Contains(requestOutput, secret) {
			t.Fatalf("persisted output leaked %q: %s", secret, requestOutput)
		}
	}
	response := connectorActionToMCPResponse(nil, finished, connectors.ActionResult{Status: finished.Status, Output: finished.Output})
	if fmt.Sprint(response.Output) != fmt.Sprint(finished.Output) {
		t.Fatalf("MCP output drifted from persisted projection: response=%#v finished=%#v", response.Output, finished.Output)
	}
}

func TestConnectorActionResultRejectsOversizedTypedOutput(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	_, err := (&Server{}).redactConnectorActionResult(t.Context(), runtime, connectors.ActionResult{
		Output: []typedConnectorResultItem{{Message: strings.Repeat("x", actionresult.MaxStringBytes+1)}},
	})
	if !errors.Is(err, actionresult.ErrInvalidOutput) {
		t.Fatalf("oversized typed output error = %v", err)
	}
	if status := connectorActionExecutionFailureStatus(err); status != connectors.ResultOutcomeUnknown {
		t.Fatalf("invalid output status = %s", status)
	}
	if status := connectorActionExecutionFailureStatus(errors.New("connection failed")); status != connectors.ResultFailed {
		t.Fatalf("ordinary execution error status = %s", status)
	}
	unknown := connectors.ClassifyActionError(
		"command_outcome_unknown",
		connectors.ResultOutcomeUnknown,
		map[string]any{"command_dispatched": true, "retry_safe": false, "output_withheld": true},
		errors.New("authorization changed after dispatch"),
	)
	if status := connectorActionExecutionFailureStatus(unknown); status != connectors.ResultOutcomeUnknown {
		t.Fatalf("dispatched command status = %s", status)
	}
	output, ok := connectorActionFailureOutput(unknown).(map[string]any)
	if !ok || output["command_dispatched"] != true || output["retry_safe"] != false || output["output_withheld"] != true {
		t.Fatalf("dispatched command failure metadata = %#v", output)
	}
}

func TestConnectorActionResultPreservesDeclaredTemporaryCapability(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	signedURL := "https://s3.example.test/object?X-Amz-Security-Token=session-token&X-Amz-Signature=signature"

	redacted, err := server.redactConnectorActionResult(context.Background(), runtime, connectors.ActionResult{
		Output: map[string]any{
			"url":               signedURL,
			"urls":              []string{signedURL},
			"capability_meta":   map[string]any{"token": "nested-must-redact"},
			"source_url":        "https://example.test/callback?token=must-redact",
			"string_map":        map[string]string{"token": "map-must-redact"},
			"secret_access_key": "must-not-leak",
		},
	}, connectors.OutputHint{TemporaryCapabilityFields: []string{"url", "urls", "capability_meta"}})
	if err != nil {
		t.Fatal(err)
	}

	output := redacted.Output.(map[string]any)
	if output["url"] != signedURL {
		t.Fatalf("temporary capability was corrupted: %q", output["url"])
	}
	if output["secret_access_key"] != "[REDACTED]" {
		t.Fatalf("credential field was not redacted: %#v", output["secret_access_key"])
	}
	if urls, ok := output["urls"].([]any); !ok || len(urls) != 1 || urls[0] != signedURL {
		t.Fatalf("temporary capability list was corrupted: %#v", output["urls"])
	}
	if sourceURL := fmt.Sprint(output["source_url"]); strings.Contains(sourceURL, "must-redact") {
		t.Fatalf("undeclared suffix field bypassed redaction: %q", sourceURL)
	}
	if metadata := fmt.Sprint(output["capability_meta"]); strings.Contains(metadata, "nested-must-redact") {
		t.Fatalf("non-string capability-shaped output bypassed redaction: %q", metadata)
	}
	if stringMap := fmt.Sprint(output["string_map"]); strings.Contains(stringMap, "map-must-redact") {
		t.Fatalf("string map bypassed redaction: %q", stringMap)
	}
}

func TestFinishConnectorActionRequestRedactsErrorAndHistory(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	request, err := store.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        postgresconnector.Kind,
		ActionName:           postgresconnector.ActionQueryReadonly,
		Input:                map[string]any{"sql": "select 1"},
		EncryptedPayloadJSON: "encrypted",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert action request: %v", err)
	}
	finished, err := server.finishConnectorActionRequest(
		context.Background(),
		runtime,
		request.ID,
		connectors.ResultFailed,
		map[string]any{
			"rows": []map[string]any{
				{
					"customer_secret": "visible-only-if-buggy",
					"token":           "token-value",
					"access_token":    "access-token-value",
					"password_hash":   "password-hash-value",
					"api_token_hash":  "api-token-hash-value",
					"secret_value":    "secret-value",
					"name":            "safe",
				},
			},
		},
		"",
		"connect failed password=super-secret Bearer abcdefghijklmnopqrstuvwxyz",
		connectors.OutputHint{SensitiveFields: []string{"customer_secret"}},
	)
	if err != nil {
		t.Fatalf("finish action request: %v", err)
	}
	if strings.Contains(finished.Error, "super-secret") || strings.Contains(finished.Error, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("finished error leaked secret: %q", finished.Error)
	}
	if !strings.Contains(finished.Error, "password=[REDACTED]") || !strings.Contains(finished.Error, "Bearer [REDACTED]") {
		t.Fatalf("finished error was not redacted as expected: %q", finished.Error)
	}
	var historyError string
	if err := database.QueryRow(`
		SELECT error
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		request.ID,
	).Scan(&historyError); err != nil {
		t.Fatalf("read history error: %v", err)
	}
	if historyError != finished.Error {
		t.Fatalf("history error drifted from finished request: history=%q finished=%q", historyError, finished.Error)
	}
	response := connectorActionToMCPResponse(nil, finished, connectors.ActionResult{Status: connectors.ResultFailed, Error: finished.Error})
	if strings.Contains(response.Error, "super-secret") || strings.Contains(response.Error, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("mcp response leaked secret: %q", response.Error)
	}
	if response.Error != finished.Error {
		t.Fatalf("mcp response error drifted from finished request: response=%q finished=%q", response.Error, finished.Error)
	}
	var outputJSON string
	if err := database.QueryRow(`
		SELECT output_json
		FROM connector_action_requests
		WHERE id = ?`,
		request.ID,
	).Scan(&outputJSON); err != nil {
		t.Fatalf("read connector action output: %v", err)
	}
	for _, secret := range []string{"visible-only-if-buggy", "token-value", "access-token-value", "password-hash-value", "api-token-hash-value", "secret-value"} {
		if strings.Contains(outputJSON, secret) {
			t.Fatalf("structured connector output leaked sensitive field value %q: %s", secret, outputJSON)
		}
	}
	for _, marker := range []string{`"customer_secret":"[REDACTED]"`, `"token":"[REDACTED]"`, `"access_token":"[REDACTED]"`, `"password_hash":"[REDACTED]"`, `"api_token_hash":"[REDACTED]"`, `"secret_value":"[REDACTED]"`} {
		if !strings.Contains(outputJSON, marker) {
			t.Fatalf("structured connector output missing redacted marker %s: %s", marker, outputJSON)
		}
	}
	if !strings.Contains(outputJSON, `"name":"safe"`) {
		t.Fatalf("structured connector output leaked sensitive field values: %s", outputJSON)
	}
}

func TestFinishConnectorActionRequestIgnoresCanceledRequestContext(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
		Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	finished, err := server.finishConnectorActionRequest(canceled, runtime, request.ID, connectors.ResultCompleted, map[string]any{"ok": true}, "done", "")
	if err != nil {
		t.Fatalf("finish with canceled request context: %v", err)
	}
	if finished.Status != connectors.ResultCompleted {
		t.Fatalf("status=%s", finished.Status)
	}
}

func TestCaptureConnectorActionSessionHandleIgnoresCanceledRequestContext(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
		Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	updated, err := server.captureConnectorActionSessionHandleIfReturned(canceled, runtime, request, connectors.ActionHandles{
		SessionID: 41, SessionGeneration: 2,
	})
	if err != nil {
		t.Fatalf("capture with canceled request context: %v", err)
	}
	if updated.SessionID == nil || *updated.SessionID != 41 || updated.SessionGeneration == nil || *updated.SessionGeneration != 2 {
		t.Fatalf("unexpected session handle: %#v", updated)
	}
	var auditedTokenID int64
	if err := database.QueryRow(`SELECT token_id FROM audit_outbox WHERE action = 'connector_action.session_handle.updated'`).Scan(&auditedTokenID); err != nil {
		t.Fatal(err)
	}
	if auditedTokenID != tokenID {
		t.Fatalf("audit token_id=%d, want %d", auditedTokenID, tokenID)
	}
}

func TestFinishConnectorActionRequestDoesNotAuditLateCompletion(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
		Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.finishConnectorActionRequest(t.Context(), runtime, request.ID, connectors.ResultCompleted, nil, "done", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := server.finishConnectorActionRequest(t.Context(), runtime, request.ID, connectors.ResultFailed, nil, "", "late failure"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action IN ('connector_action.request.completed', 'connector_action.request.failed')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("terminal audit events=%d, want 1", count)
	}
}

func TestRecoverOrphanedConnectorActionsPreservesActiveExecutions(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := connectorActionTestRuntime(t, database, secretVault)
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	createRunning := func() connectortargets.ActionRequest {
		request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
			TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
			ConnectorKind: postgresconnector.Kind, ActionName: postgresconnector.ActionQueryReadonly,
			Status: connectors.ResultRunning,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE connector_action_requests SET created_at = datetime('now', '-2 minutes') WHERE id = ?`, request.ID); err != nil {
			t.Fatal(err)
		}
		return request
	}
	orphaned := createRunning()
	active := createRunning()
	leased := createRunning()
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET execution_owner = ?, execution_lease_expires_at = ?
		WHERE id = ?`, "live-runtime", time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), leased.ID); err != nil {
		t.Fatal(err)
	}
	runtime.setConnectorCredentialBoundary(active.ID, connectorCredentialBoundary{})

	server.recoverOrphanedConnectorActions(t.Context(), runtime, time.Now().UTC())

	gotOrphaned, err := store.GetActionRequest(t.Context(), orphaned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOrphaned.Status != connectors.ResultOutcomeUnknown || gotOrphaned.Error != connectorActionPersistenceUnknownMessage {
		t.Fatalf("orphaned request was not recovered: %#v", gotOrphaned)
	}
	gotActive, err := store.GetActionRequest(t.Context(), active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotActive.Status != connectors.ResultRunning {
		t.Fatalf("active request was recovered prematurely: %#v", gotActive)
	}
	gotLeased, err := store.GetActionRequest(t.Context(), leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotLeased.Status != connectors.ResultRunning {
		t.Fatalf("leased request was recovered prematurely: %#v", gotLeased)
	}

	staleSelection := createRunning()
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET execution_owner = ?, execution_lease_expires_at = ?
		WHERE id = ?`, "live-runtime", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), staleSelection.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a recovery worker that selected the request before dispatch
	// renewed its lease. The terminal CAS must observe the renewed lease.
	if _, err := database.Exec(`
		UPDATE connector_action_requests
		SET execution_lease_expires_at = ?
		WHERE id = ?`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), staleSelection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.persistExpiredConnectorActionRecovery(t.Context(), runtime, staleSelection.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	gotRenewed, err := store.GetActionRequest(t.Context(), staleSelection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRenewed.Status != connectors.ResultRunning {
		t.Fatalf("renewed request was terminalized by stale recovery selection: %#v", gotRenewed)
	}
}
