package gatewayconnectoractions

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type approvalFixture struct {
	component *Component
	workspace Workspace
	store     *connectortargets.Store
	connector *approvalConnector
	profile   connectortargets.CredentialProfile
	tokenID   int64
	call      Call
}

func newApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "approval.db"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	tokenStore := tokens.NewStore(database)
	token, err := tokenStore.Create(t.Context(), tokens.CreateRequest{Name: "Approval fixture"})
	if err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "Approval target", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "test", Label: "default",
		Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	connector := &approvalConnector{}
	registry := connectors.NewRegistry()
	if err := registry.Register(connector); err != nil {
		t.Fatal(err)
	}
	secretVault, err := vault.New("approval fixture record material")
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{
		Storage: ActionStorage{
			Database: database, Tokens: tokenStore, Registry: registry.Snapshot(),
			SecretVault: secretVault, WorkspaceID: "approval-fixture",
		},
		Identity: ActionIdentity{
			RuntimeInstanceID: "approval-fixture-runtime", MCPStarted: func() bool { return true },
			Ensure: func() error { return nil }, Tag: func(value []byte) (string, error) {
				hash := sha256.Sum256(value)
				return hex.EncodeToString(hash[:]), nil
			},
		},
		Workflow: approvalWorkflowPorts(t, database),
	}
	component := New(Dependencies{MaxJSONBytes: 64 << 10, SupportsRunning: func(PreparedRequest) bool { return false }})
	t.Cleanup(func() { component.ReleaseWorkspace(workspace) })
	fixture := approvalFixture{
		component: component, workspace: workspace, store: store, connector: connector,
		profile: profile, tokenID: token.ID,
		call: Call{
			TokenID: token.ID, TargetRef: connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
			ActionName: "inspect", Input: map[string]any{"value": "approval-payload"}, Reason: "Verify approval authority",
		},
	}
	fixture.permission(t, connectortargets.ActionPermissionApprovalRequired)
	return fixture
}

func approvalWorkflowPorts(t *testing.T, database *sql.DB) WorkflowPorts {
	t.Helper()
	identity := func(value string) string { return value }
	coordinator := observability.NewCoordinator(database, nil, identity, nil)
	return WorkflowPorts{
		AcquireSecret: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() },
		RedactBasic:   func(_ context.Context, value string) string { return value },
		RedactCustom:  func(_ context.Context, value string) string { return value },
		Mutate:        coordinator.WithMutation,
		Transaction: func(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
			return coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
				return mutate(tx, AuditAppender(appendAudit))
			})
		},
		Observe: func(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, value any) {
			if err := coordinator.WriteRequired(ctx, actor, tokenID, runtimeID, action, value); err != nil {
				t.Errorf("observe %s: %v", action, err)
			}
		},
		Capabilities: func(string, []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver { return nil },
		FinishRunning: func(context.Context, int64, PreparedRequest, executionprincipal.Principal, connectors.ActionHandles) {
			t.Error("synchronous approval fixture unexpectedly returned running")
		},
	}
}

func (fixture approvalFixture) permission(t *testing.T, rule connectortargets.ActionPermissionRule) {
	t.Helper()
	if err := fixture.store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID: fixture.tokenID, TargetID: fixture.profile.TargetID, ProfileID: fixture.profile.ID,
		ActionName: fixture.call.ActionName, ExecutionRule: rule,
	}); err != nil {
		t.Fatal(err)
	}
}

type approvalConnector struct{ dispatches atomic.Int64 }

func (*approvalConnector) Kind() string                                     { return "fixture" }
func (*approvalConnector) Label() string                                    { return "Approval fixture" }
func (*approvalConnector) Version() string                                  { return "1" }
func (*approvalConnector) TargetSchema() connectors.Schema                  { return connectors.Schema{} }
func (*approvalConnector) CredentialSchemas() []connectors.CredentialSchema { return nil }
func (*approvalConnector) GetHelp(context.Context, connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{}, nil
}
func (*approvalConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name: "inspect", Label: "Inspect", Description: "Inspect an in-memory fixture", Risk: connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "value", Type: connectors.FieldString, Required: true}}},
	}}, nil
}
func (*approvalConnector) PrepareAction(_ context.Context, request connectors.ActionRequest) (connectors.PreparedAction, error) {
	return connectors.PreparedAction{
		ConnectorKind: "fixture", TargetRef: request.Target.Ref, ProfileID: request.Profile.ID,
		ActionName: request.ActionName, Risk: connectors.RiskRead, Title: "Inspect fixture",
		Payload: map[string]any{"value": request.Input["value"]}, Preview: map[string]any{"value": request.Input["value"]},
	}, nil
}
func (connector *approvalConnector) ExecuteAction(_ context.Context, _ connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	connector.dispatches.Add(1)
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: action.Payload}, nil
}
