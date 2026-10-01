package projects_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projects/scopes"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestTokenCreationRollsBackScopeFailureAndAllowsRetry(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "create-rollback.aipdb"), "ScopePolicyFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TRIGGER scope_fault BEFORE INSERT ON token_project_scopes BEGIN SELECT RAISE(ABORT, 'scope-write-failed'); END`); err != nil {
		t.Fatal(err)
	}
	store := tokens.NewStore(database)
	if _, err := store.Create(t.Context(), tokens.CreateRequest{Name: "rollback-token"}); err == nil || !strings.Contains(err.Error(), "scope-write-failed") {
		t.Fatalf("scope failure not returned: %v", err)
	}
	for _, table := range []string{"api_tokens", "token_project_scopes"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed Create left %s rows: %d %v", table, count, err)
		}
	}
	if _, err := database.Exec(`DROP TRIGGER scope_fault`); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(t.Context(), tokens.CreateRequest{Name: "rollback-token"})
	if err != nil {
		t.Fatalf("clean retry failed: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM token_project_scopes WHERE token_id = ? AND enabled = 1`, created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("clean retry lost project admission: %d %v", count, err)
	}
}

func TestTokenCreationInitializesOnlyActiveProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scope-policy.aipdb")
	database, err := db.OpenEncrypted(path, "ScopePolicyFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := projects.NewStore(database)
	active, err := store.Create(t.Context(), "Active Project")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := store.Create(t.Context(), "Archived Project")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Archive(t.Context(), archived.ID); err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "scope-policy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.OpenEncrypted(path, "ScopePolicyFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if allowed, err := projects.NewStore(reopened).TokenCanAccessProject(t.Context(), token.ID, active.ID); err != nil || !allowed {
		t.Fatalf("new token lost active project admission: %v", err)
	}
	var count int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM token_project_scopes WHERE token_id = ? AND project_id = ?`, token.ID, archived.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("new token granted archived project: %d %v", count, err)
	}
}

func TestInitializationRemainsInCallerOwnedTransaction(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "scope-rollback.aipdb"), "ScopePolicyFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO api_tokens (id, name, token_hash, token_prefix, created_at, updated_at)
		VALUES (7, 'fixture-token', 'fixture-hash', 'fixture', 'created', 'updated')`); err != nil {
		t.Fatal(err)
	}
	if err := scopes.InitializeForToken(t.Context(), tx, 7, "created", "updated"); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM token_project_scopes WHERE token_id = 7 AND enabled = 1`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("scope not written in caller transaction: %d %v", pending, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, record := range []struct{ table, key string }{{"api_tokens", "id"}, {"token_project_scopes", "token_id"}} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + record.table + " WHERE " + record.key + " = 7").Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback left %s initialized: %d %v", record.table, count, err)
		}
	}
}
