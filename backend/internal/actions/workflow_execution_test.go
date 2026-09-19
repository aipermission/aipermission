package actions

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type workflowExecutionDelivery struct {
	mu     sync.Mutex
	active int
}

func (d *workflowExecutionDelivery) Acquire(context.Context) (func(), error) {
	d.mu.Lock()
	d.active++
	d.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			d.active--
			d.mu.Unlock()
		})
	}, nil
}

func (d *workflowExecutionDelivery) held() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active > 0
}

type workflowExecutionTokenReader struct{ token AuthorizationToken }

func (r workflowExecutionTokenReader) Get(context.Context, int64, time.Time) (AuthorizationToken, error) {
	return r.token, nil
}

type workflowExecutionMutations struct {
	database *sql.DB
	delivery *workflowExecutionDelivery
	held     chan bool
}

func (m workflowExecutionMutations) WithMutation(
	ctx context.Context,
	_ string,
	_ *int64,
	_ int64,
	action string,
	_ func() any,
	mutate func(*sql.Tx) error,
) error {
	if action == "connector_action.request.created" {
		m.held <- m.delivery.held()
	}
	tx, err := m.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (m workflowExecutionMutations) WithTransaction(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
	tx, err := m.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx, func(*sql.Tx, string, *int64, int64, string, any) error { return nil }); err != nil {
		return err
	}
	return tx.Commit()
}

func (workflowExecutionMutations) Observe(context.Context, string, *int64, int64, string, any) {}

func TestCallHoldsDeliveryLeaseThroughAuthorizationAndRequestInsertion(t *testing.T) {
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "workflow.db"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	storedToken, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "lease-regression"})
	if err != nil {
		t.Fatal(err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "Lease fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind, Kind: "test", Label: "default",
		EncryptedSecretJSON: "{}", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetActionPermission(t.Context(), connectortargets.SetActionPermissionInput{
		TokenID: storedToken.ID, TargetID: target.ID, ProfileID: profile.ID, ActionName: "query_readonly",
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatal(err)
	}
	targetView, profileView, err := store.ResolveConnectorActionTarget(
		t.Context(), connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
	)
	if err != nil {
		t.Fatal(err)
	}
	registry := connectors.NewRegistry()
	if err := registry.Register(&prepareConnector{kind: target.ConnectorKind}); err != nil {
		t.Fatal(err)
	}
	delivery := &workflowExecutionDelivery{}
	held := make(chan bool, 1)
	dependencies := workflowTestDependencies(delivery)
	dependencies.Database = database
	dependencies.Tokens = workflowExecutionTokenReader{token: AuthorizationToken{ID: storedToken.ID, Active: true}}
	dependencies.Registry = registry
	dependencies.Targets = &fakeResolver{target: targetView, profile: profileView}
	dependencies.Mutations = workflowExecutionMutations{database: database, delivery: delivery, held: held}
	runtime, err := NewRuntime(dependencies)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runtime.Call(t.Context(), Call{
		TokenID: storedToken.ID, Source: SourceMCP,
		TargetRef:  connectors.FormatTargetRef(target.ConnectorKind, target.ID, profile.ID),
		ActionName: "query_readonly", Input: map[string]any{"sql": "select 1"}, Reason: "lease regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.Status != connectors.ResultApprovalPending {
		t.Fatalf("result status = %q, want approval_pending", result.Result.Status)
	}
	if !<-held {
		t.Fatal("delivery lease was released before the approval request became durable")
	}
	if delivery.held() {
		t.Fatal("delivery lease remained held after the pending result returned")
	}
}
