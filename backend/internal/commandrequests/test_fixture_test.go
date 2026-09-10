package commandrequests

import (
	"database/sql"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func commandRequestFixture(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "commands.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projectstore.NewStore(database).Create(t.Context(), "Command Project")
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
	return database, runtimeID
}
