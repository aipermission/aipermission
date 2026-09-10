package vaultsessions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

type observerLeaseAuthorizer struct {
	err     error
	calls   int
	session console.SessionAuthorization
}

func (a *observerLeaseAuthorizer) Authorize(
	_ context.Context,
	_ executionprincipal.Principal,
	session console.SessionAuthorization,
	operation console.SessionOperation,
) error {
	a.calls++
	a.session = session
	if operation != console.OperationObserve {
		return errors.New("unexpected operation")
	}
	return a.err
}

func TestObserverRequiresVaultLeaseOnlyForEnvironmentSessions(t *testing.T) {
	database, runtimeID, sessionID := observerSessionFixture(t)
	principal, err := executionprincipal.MCPToken(7, "workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	leases := &observerLeaseAuthorizer{}
	observer := NewObserver(database, leases)
	plain := ObserveRequest{SessionID: sessionID, SessionGeneration: 1, ExpectedRuntimeID: runtimeID}
	if !observer.Authorized(t.Context(), principal, plain) {
		t.Fatal("plain session should be observable without a Vault environment lease")
	}
	plain.RequireEnvironment = true
	if observer.Authorized(t.Context(), principal, plain) {
		t.Fatal("plain session unexpectedly satisfied the environment requirement")
	}
	if leases.calls != 0 {
		t.Fatalf("plain session consulted leases %d times", leases.calls)
	}

	if _, err := database.ExecContext(t.Context(), `
		UPDATE console_sessions
		SET environment_content_hash = 'environment', approval_context_hash = 'approval'
		WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	if !observer.Authorized(t.Context(), principal, plain) {
		t.Fatal("Vault environment session should be observable with its lease")
	}
	if leases.calls != 1 || leases.session.Handle.RuntimeID != runtimeID ||
		leases.session.EnvironmentContentHash != "environment" || leases.session.ApprovalContextHash != "approval" {
		t.Fatalf("unexpected lease authorization: calls=%d session=%+v", leases.calls, leases.session)
	}
	leases.err = ErrUnauthorized
	if observer.Authorized(t.Context(), principal, plain) {
		t.Fatal("Vault environment session unexpectedly bypassed a rejected lease")
	}
}

func TestObserverRejectsMismatchedOrInactiveSessionIdentity(t *testing.T) {
	database, runtimeID, sessionID := observerSessionFixture(t)
	principal, err := executionprincipal.MCPToken(7, "workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	observer := NewObserver(database, &observerLeaseAuthorizer{})
	request := ObserveRequest{SessionID: sessionID, SessionGeneration: 1, ExpectedRuntimeID: runtimeID + 1}
	if observer.Authorized(t.Context(), principal, request) {
		t.Fatal("mismatched runtime was authorized")
	}
	if _, err := database.ExecContext(t.Context(), `
		UPDATE console_sessions
		SET status = 'closed', environment_content_hash = 'environment', approval_context_hash = 'approval'
		WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	request.ExpectedRuntimeID = runtimeID
	request.RequireEnvironment = true
	if observer.Authorized(t.Context(), principal, request) {
		t.Fatal("closed session was authorized")
	}
}

func observerSessionFixture(t *testing.T) (*sql.DB, int64, int64) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "observer.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projectstore.NewStore(database).Create(t.Context(), "Observer Project")
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_targets (project_id, connector_kind, name, status, config_json, created_at, updated_at)
		VALUES (?, 'test', 'target', 'active', '{}', datetime('now'), datetime('now'))`, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'profile', '{}', 'active', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO connector_runtime_surfaces (connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, 'live_console', 'console', 'active', datetime('now'), datetime('now'))`, targetID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.ExecContext(t.Context(), `
		INSERT INTO console_sessions (runtime_id, name, status, generation, created_at, updated_at)
		VALUES (?, 'session', 'connected', 1, datetime('now'), datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return database, runtimeID, sessionID
}
