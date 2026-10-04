package actions

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

const nativeWorkflowSecret = "workflow-private-credential-with-spaces 123"

type nativeWorkflowFixture struct {
	runtime   *Runtime
	store     *connectortargets.Store
	database  *sql.DB
	connector *nativeWorkflowConnector
	delivery  *workflowExecutionDelivery
	call      Call
}

type nativeWorkflowConnector struct {
	prepareConnector
	dispatches atomic.Int64
	risk       connectors.RiskLevel
	execute    func(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error)
}

func (connector *nativeWorkflowConnector) PrepareAction(ctx context.Context, request connectors.ActionRequest) (connectors.PreparedAction, error) {
	prepared, err := connector.prepareConnector.PrepareAction(ctx, request)
	prepared.Payload = map[string]any{"sql": request.Input["sql"]}
	prepared.Preview = map[string]any{"sql": request.Input["sql"]}
	prepared.Risk = connector.risk
	return prepared, err
}

func (connector *nativeWorkflowConnector) ExecuteAction(ctx context.Context, runtime connectors.RuntimeContext, prepared connectors.PreparedAction) (connectors.ActionResult, error) {
	connector.dispatches.Add(1)
	if connector.execute != nil {
		return connector.execute(ctx, runtime, prepared)
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"rows": 1}}, nil
}

func newNativeWorkflowFixture(t *testing.T, rule connectortargets.ActionPermissionRule, risk connectors.RiskLevel) nativeWorkflowFixture {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "actions.aipdb"), "DisposableWorkflowPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close native workflow: %v", err)
		}
	})
	tokenStore := tokens.NewStore(database)
	token, err := tokenStore.Create(t.Context(), tokens.CreateRequest{Name: "Native workflow"})
	if err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "fixture", Name: "Workflow target", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "test", Label: "default", Public: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	secretVault, err := vault.New("Disposable native workflow record material")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := recordcrypto.EncryptJSON(secretVault, "native-workflow", recordcrypto.ConnectorCredentialProfile, profile.ID, map[string]any{"password": nativeWorkflowSecret})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE connector_credential_profiles SET encrypted_secret_json = ? WHERE id = ?`, sealed, profile.ID); err != nil {
		t.Fatal(err)
	}
	actionName := "inspect"
	if risk == connectors.RiskWrite {
		actionName = "mutate"
	}
	if err := store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{TokenID: token.ID, TargetID: target.ID, ProfileID: profile.ID, ActionName: actionName, ExecutionRule: rule}); err != nil {
		t.Fatal(err)
	}
	connector := &nativeWorkflowConnector{risk: risk, prepareConnector: prepareConnector{kind: target.ConnectorKind, actions: []connectors.ActionDefinition{{Name: actionName, Label: "Native fixture action", Description: "In-memory workflow boundary probe", Risk: risk, InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "sql", Type: connectors.FieldString, Required: true}}}}}}}
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	delivery := &workflowExecutionDelivery{}
	dependencies := workflowTestDependencies(delivery)
	dependencies.Database, dependencies.Registry, dependencies.Tokens = database, registry, nativeWorkflowTokens{tokenStore}
	dependencies.Targets, dependencies.SealedRecords = nativeWorkflowTargets{store}, nativeWorkflowRecords{secretVault}
	dependencies.Mutations = nativeWorkflowMutations{Coordinator: observability.NewCoordinator(database, nil, func(value string) string { return value }, nil), t: t}
	dependencies.Redactor, err = actionresult.NewRedactor(func(_ context.Context, value string) string { return value }, func(_ context.Context, value string) string { return value }, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtime.StopRecovery()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.StopFinalizers(ctx); err != nil {
			t.Errorf("drain native workflow: %v", err)
		}
		if delivery.held() {
			t.Error("native workflow leaked delivery admission")
		}
	})
	return nativeWorkflowFixture{runtime: runtime, store: store, database: database, connector: connector, delivery: delivery, call: Call{Source: SourceMCP, TokenID: token.ID, TargetRef: connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID), ActionName: actionName, Input: map[string]any{"sql": "select 1"}, Reason: "Verify native workflow", IdempotencyKey: "native-workflow-call"}}
}

func (fixture nativeWorkflowFixture) historyStatus(t *testing.T, id int64, want connectors.ResultStatus) {
	t.Helper()
	var status string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT status FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, id).Scan(&status); err != nil || status != string(want) {
		t.Fatalf("durable history projection: %q want=%q error=%v", status, want, err)
	}
}
