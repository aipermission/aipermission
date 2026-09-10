package commandrequests

import (
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestStoreGetBuildsStableCommandPresentation(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "commands.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := projectstore.NewStore(database).Create(t.Context(), "Project")
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
		INSERT INTO command_requests (runtime_id, source, command, reason, status, stdout, created_at)
		VALUES (?, '', 'rm -rf /tmp/example', 'cleanup', 'running', char(27) || '[31mworking', datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	item, err := NewStore(database).Get(t.Context(), requestID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Source != SourceMCP || item.TargetName != "target" || item.Stdout != "working" {
		t.Fatalf("record = %#v", item)
	}
	if item.RetryAfterSeconds != 3 || item.AssistantHint == "" {
		t.Fatalf("running metadata = %#v", item)
	}
	if len(item.PolicyWarnings) != 1 || item.PolicyWarnings[0].Code != "destructive_file_operation" {
		t.Fatalf("policy warnings = %#v", item.PolicyWarnings)
	}
}

func TestStoreGetHonorsSourceAndTokenScope(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "scope.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := NewStore(database)
	if _, err := store.Get(t.Context(), 999, 1, SourceMCP); err == nil {
		t.Fatal("missing scoped request unexpectedly resolved")
	}
	if _, err := NewStore(nil).Get(t.Context(), 1, 0, ""); err == nil {
		t.Fatal("missing persistence unexpectedly succeeded")
	}
}
