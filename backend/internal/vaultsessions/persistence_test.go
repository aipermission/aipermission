package vaultsessions

import (
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestPersistenceGrantsReplacesAndRevokesLeases(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "leases.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := projectstore.NewStore(database).Create(t.Context(), "Lease Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_targets (project_id, connector_kind, name, status, config_json, created_at, updated_at)
		VALUES (?, 'test', 'target', 'active', '{}', datetime('now'), datetime('now'))`, project.ID); err != nil {
		t.Fatal(err)
	}
	var targetID int64
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM connector_targets WHERE name = 'target'`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'profile', '{}', 'active', datetime('now'), datetime('now'))`, targetID); err != nil {
		t.Fatal(err)
	}
	var profileID int64
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM connector_credential_profiles WHERE target_id = ?`, targetID).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
		INSERT INTO connector_runtime_surfaces (connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, 'live_console', 'console', 'active', datetime('now'), datetime('now'))`, targetID, profileID); err != nil {
		t.Fatal(err)
	}
	var runtimeID int64
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM connector_runtime_surfaces WHERE target_id = ?`, targetID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
		INSERT INTO console_sessions (runtime_id, name, status, generation, created_at, updated_at)
		VALUES (?, 'session', 'connected', 1, datetime('now'), datetime('now'))`, runtimeID); err != nil {
		t.Fatal(err)
	}
	var sessionID int64
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM console_sessions WHERE runtime_id = ?`, runtimeID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	persistence := NewPersistence(database)
	lease := Lease{
		TokenID: token.ID, RuntimeID: runtimeID, SessionID: sessionID, SessionGeneration: 1,
		ApprovalContextHash: "approval", EnvironmentContentHash: "environment",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := persistence.Grant(t.Context(), project.ID, lease); err != nil {
		t.Fatal(err)
	}
	lease.EnvironmentContentHash = "environment-two"
	if err := persistence.Grant(t.Context(), project.ID, lease); err != nil {
		t.Fatal(err)
	}
	var status, environment string
	if err := database.QueryRowContext(t.Context(), `
		SELECT status, environment_content_hash FROM vault_session_leases
		WHERE session_id = ? AND session_generation = ?`, sessionID, 1,
	).Scan(&status, &environment); err != nil {
		t.Fatal(err)
	}
	if status != "active" || environment != "environment-two" {
		t.Fatalf("status=%q environment=%q", status, environment)
	}
	if err := persistence.Revoke(t.Context(), sessionID, 1); err != nil {
		t.Fatal(err)
	}
	if err := persistence.Grant(t.Context(), project.ID, lease); err != nil {
		t.Fatal(err)
	}
	if err := persistence.RevokeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `
		SELECT status FROM vault_session_leases WHERE session_id = ? AND session_generation = ?`, sessionID, 1,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "revoked" {
		t.Fatalf("status = %q", status)
	}
}

func TestPersistenceSelectsVaultInvalidationScopes(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "invalidation.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := projectstore.NewStore(database).Create(t.Context(), "Invalidation Project")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "codex"})
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
		INSERT INTO console_sessions (
			runtime_id, name, status, generation, environment_content_hash, created_at, updated_at
		) VALUES (?, 'Vault session', 'connected', 3, 'environment', datetime('now'), datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	lease := Lease{
		TokenID: token.ID, RuntimeID: runtimeID, SessionID: sessionID, SessionGeneration: 3,
		ApprovalContextHash: "approval", EnvironmentContentHash: "environment",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	persistence := NewPersistence(database)
	if err := persistence.Grant(t.Context(), project.ID, lease); err != nil {
		t.Fatal(err)
	}

	runtimeIDs, err := persistence.RuntimeIDsForTargetProfile(t.Context(), targetID, profileID)
	if err != nil || len(runtimeIDs) != 1 || runtimeIDs[0] != runtimeID {
		t.Fatalf("target profile runtimes = %v, %v", runtimeIDs, err)
	}
	allRuntimeIDs, err := persistence.AllRuntimeIDs(t.Context())
	if err != nil || len(allRuntimeIDs) != 1 || allRuntimeIDs[0] != runtimeID {
		t.Fatalf("all runtimes = %v, %v", allRuntimeIDs, err)
	}
	projectSessions, err := persistence.ActiveForProject(t.Context(), project.ID)
	if err != nil || len(projectSessions) != 1 || projectSessions[0].SessionID != sessionID || projectSessions[0].Generation != 3 {
		t.Fatalf("project sessions = %#v, %v", projectSessions, err)
	}
	runtimeSessions, err := persistence.ActiveEnvironmentSessionsForRuntimes(t.Context(), []int64{0, runtimeID, runtimeID})
	if err != nil || len(runtimeSessions) != 1 || runtimeSessions[0].SessionID != sessionID {
		t.Fatalf("runtime sessions = %#v, %v", runtimeSessions, err)
	}
}

func TestPersistenceRejectsMissingDatabase(t *testing.T) {
	if err := NewPersistence(nil).Grant(t.Context(), 1, Lease{}); err != ErrPersistenceUnavailable {
		t.Fatalf("Grant() error = %v", err)
	}
	if err := NewPersistence(nil).Revoke(t.Context(), 1, 1); err != ErrPersistenceUnavailable {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := NewPersistence(nil).RevokeAll(t.Context()); err != ErrPersistenceUnavailable {
		t.Fatalf("RevokeAll() error = %v", err)
	}
	if _, err := NewPersistence(nil).ActiveForProject(t.Context(), 1); err != ErrPersistenceUnavailable {
		t.Fatalf("ActiveForProject() error = %v", err)
	}
	if _, err := NewPersistence(nil).RuntimeIDsForTargetProfile(t.Context(), 1, 1); err != ErrPersistenceUnavailable {
		t.Fatalf("RuntimeIDsForTargetProfile() error = %v", err)
	}
	if _, err := NewPersistence(nil).AllRuntimeIDs(t.Context()); err != ErrPersistenceUnavailable {
		t.Fatalf("AllRuntimeIDs() error = %v", err)
	}
	if _, err := NewPersistence(nil).ActiveEnvironmentSessionsForRuntimes(t.Context(), []int64{1}); err != ErrPersistenceUnavailable {
		t.Fatalf("ActiveEnvironmentSessionsForRuntimes() error = %v", err)
	}
}
