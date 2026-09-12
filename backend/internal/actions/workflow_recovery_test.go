package actions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

type recoveryTestMutations struct {
	database    *sql.DB
	appendAudit AuditAppender
}

func (m recoveryTestMutations) WithMutation(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error {
	return nil
}

func (m recoveryTestMutations) WithTransaction(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
	tx, err := m.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	appendAudit := m.appendAudit
	if appendAudit == nil {
		appendAudit = func(*sql.Tx, string, *int64, int64, string, any) error { return nil }
	}
	if err := mutate(tx, appendAudit); err != nil {
		return err
	}
	return tx.Commit()
}

func (recoveryTestMutations) Observe(context.Context, string, *int64, int64, string, any) {}

func TestMarkRunningOutcomeUnknownDoesNotParseUntrustedActionPayload(t *testing.T) {
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "recovery.db"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close recovery database: %v", err)
		}
	})
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "malformed action target", Config: map[string]any{},
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
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
		ActionName: "test", Input: map[string]any{"value": "safe"}, Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE connector_action_requests SET encrypted_payload_json = '{' WHERE id = ?`, request.ID); err != nil {
		t.Fatal(err)
	}

	dependencies := workflowTestDependencies(&workflowTestDelivery{})
	dependencies.Database = database
	var auditPayload map[string]any
	dependencies.Mutations = recoveryTestMutations{
		database: database,
		appendAudit: func(_ *sql.Tx, _ string, _ *int64, _ int64, _ string, payload any) error {
			auditPayload, _ = payload.(map[string]any)
			return nil
		},
	}
	runtime, err := NewRuntime(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.MarkRunningOutcomeUnknown(t.Context(), "workspace locked during execution"); err != nil {
		t.Fatalf("recover malformed running action: %v", err)
	}

	var status, errorText string
	if err := database.QueryRowContext(t.Context(), `SELECT status, error FROM connector_action_requests WHERE id = ?`, request.ID).Scan(&status, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != string(connectors.ResultOutcomeUnknown) || errorText != "workspace locked during execution" {
		t.Fatalf("terminal state = %q %q", status, errorText)
	}
	if err := database.QueryRowContext(t.Context(), `
		SELECT status, error
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, request.ID,
	).Scan(&status, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != string(connectors.ResultOutcomeUnknown) || errorText != "workspace locked during execution" {
		t.Fatalf("projected terminal state = %q %q", status, errorText)
	}
	for key, expected := range map[string]any{
		"project_id": target.ProjectID, "target_id": target.ID, "profile_id": profile.ID,
		"connector_kind": target.ConnectorKind, "action_name": "test",
	} {
		if auditPayload[key] != expected {
			t.Fatalf("audit payload %s = %#v, want %#v", key, auditPayload[key], expected)
		}
	}
}

func TestMarkRunningOutcomeUnknownRollsBackWhenAuditFails(t *testing.T) {
	database, request := recoveryDatabaseWithRunningRequest(t)
	auditFailure := errors.New("audit unavailable")

	dependencies := workflowTestDependencies(&workflowTestDelivery{})
	dependencies.Database = database
	dependencies.Mutations = recoveryTestMutations{
		database: database,
		appendAudit: func(*sql.Tx, string, *int64, int64, string, any) error {
			return auditFailure
		},
	}
	runtime, err := NewRuntime(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.MarkRunningOutcomeUnknown(t.Context(), "workspace locked during execution"); !errors.Is(err, auditFailure) {
		t.Fatalf("recovery error = %v, want %v", err, auditFailure)
	}

	for _, query := range []string{
		`SELECT status FROM connector_action_requests WHERE id = ?`,
		`SELECT status FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
	} {
		var status string
		if err := database.QueryRowContext(t.Context(), query, request.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != string(connectors.ResultRunning) {
			t.Fatalf("status after audit rollback = %q, want running", status)
		}
	}
}

func recoveryDatabaseWithRunningRequest(t *testing.T) (*sql.DB, connectortargets.ActionRequest) {
	t.Helper()
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "recovery-rollback.db"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close recovery database: %v", err)
		}
	})
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "fixture", Name: "rollback target", Config: map[string]any{},
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
	request, err := store.InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
		TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: target.ConnectorKind,
		ActionName: "test", Input: map[string]any{"value": "safe"}, Status: connectors.ResultRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	return database, request
}
