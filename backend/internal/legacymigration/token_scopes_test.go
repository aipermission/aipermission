package legacymigration

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func openLegacyTokenScopeFixture(t *testing.T) (*sql.DB, *sql.DB, string, string) {
	t.Helper()
	source, err := db.OpenEncryptedForMigration(filepath.Join(t.TempDir(), "legacy.aipdb"), "LegacyScopePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	secret := "migration-test-secret-01234567890123456789"
	createLegacySchema(t, source)
	insertLegacyRows(t, source, secret)
	path := filepath.Join(t.TempDir(), "target.aipdb")
	target, err := db.OpenEncrypted(path, "TargetScopePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	return source, target, path, secret
}

func TestLegacyMigrationScopesPreserveEffectivePermissionAndTokenState(t *testing.T) {
	source, target, path, secret := openLegacyTokenScopeFixture(t)
	const created = "2026-01-01T00:00:00Z"
	const updated = "2026-01-02T00:00:00Z"
	for _, state := range []struct {
		id                     int64
		name, revoked, expires string
	}{
		{8, "expired", "", "2026-01-03T00:00:00Z"},
		{9, "revoked", "2026-01-04T00:00:00Z", "2030-01-01T00:00:00Z"},
		{10, "ungranted", "", "2030-01-01T00:00:00Z"},
	} {
		if _, err := source.Exec(`INSERT INTO api_tokens (id, name, token_hash, token_prefix, revoked_at, expires_at, created_at, updated_at)
			VALUES (?, ?, ?, 'fixture', NULLIF(?, ''), NULLIF(?, ''), ?, ?)`, state.id, state.name, "hash:"+state.name, state.revoked, state.expires, created, updated); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(`UPDATE token_server_permissions SET expires_at = '2030-01-01T00:00:00Z' WHERE token_id = 7`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacyRows(t.Context(), source, target, secret); err != nil {
		t.Fatal(err)
	}
	checkMigratedTokenScopeAccess(t, target)
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.OpenEncrypted(path, "TargetScopePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	checkMigratedTokenScopeAccess(t, reopened)
	for _, id := range []int64{8, 9, 10} {
		var scopeCreated, scopeUpdated string
		if err := reopened.QueryRow(`SELECT created_at, updated_at FROM token_project_scopes WHERE token_id = ?`, id).Scan(&scopeCreated, &scopeUpdated); err != nil || scopeCreated != created || scopeUpdated != updated {
			t.Fatalf("scope timestamps changed: %q %q %v", scopeCreated, scopeUpdated, err)
		}
	}
}

func checkMigratedTokenScopeAccess(t *testing.T, database *sql.DB) {
	t.Helper()
	projectStore := projects.NewStore(database)
	project, err := projectStore.Ungrouped(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{7, 8, 9, 10} {
		if allowed, err := projectStore.TokenCanAccessProject(t.Context(), id, project.ID); err != nil || !allowed {
			t.Fatalf("imported token has no project admission: %d %v", id, err)
		}
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	store := tokens.NewStore(database)
	for _, expected := range []struct {
		id               int64
		revoked, expires string
	}{
		{8, "", "2026-01-03T00:00:00Z"}, {9, "2026-01-04T00:00:00Z", "2030-01-01T00:00:00Z"}, {10, "", "2030-01-01T00:00:00Z"},
	} {
		item, err := store.Get(t.Context(), expected.id)
		if err != nil || item.RevokedAt != expected.revoked || item.ExpiresAt != expected.expires || item.CreatedAt != "2026-01-01T00:00:00Z" || item.UpdatedAt != "2026-01-02T00:00:00Z" {
			t.Fatalf("migration changed token identity/lifecycle: %#v %v", item, err)
		}
	}
	if authentication, err := store.AuthenticateHash(t.Context(), "sha256:test", now); err != nil || authentication.ID != 7 {
		t.Fatalf("active imported token not usable: %#v %v", authentication, err)
	}
	for _, hash := range []string{"hash:expired", "hash:revoked"} {
		if _, err := store.AuthenticateHash(t.Context(), hash, now); !errors.Is(err, tokens.ErrNotFound) {
			t.Fatalf("inactive imported token authenticated: %s %v", hash, err)
		}
	}
	var targetID, profileID int64
	if err := database.QueryRow(`SELECT target_id, profile_id FROM token_connector_action_permissions WHERE token_id = 7`).Scan(&targetID, &profileID); err != nil {
		t.Fatal(err)
	}
	permissionStore := connectortargets.NewStore(database)
	permission, err := permissionStore.GetActionPermission(t.Context(), 7, targetID, profileID, "exec", now)
	if err != nil || permission.ExecutionRule != connectortargets.ActionPermissionAlwaysRun {
		t.Fatalf("imported action permission ineffective: %#v %v", permission, err)
	}
	if _, err := permissionStore.GetActionPermission(t.Context(), 10, targetID, profileID, "exec", now); !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		t.Fatalf("project admission invented an action grant: %v", err)
	}
	if _, err := permissionStore.GetActionPermission(t.Context(), 7, targetID, profileID, "exec", time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)); !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
		t.Fatalf("imported permission lost expiry: %v", err)
	}
}

func TestLegacyMigrationScopeAndLatePermissionFailuresRollbackEverything(t *testing.T) {
	for _, table := range []string{"token_project_scopes", "token_connector_action_permissions"} {
		t.Run(table, func(t *testing.T) {
			source, target, _, secret := openLegacyTokenScopeFixture(t)
			if _, err := target.Exec(fmt.Sprintf(`CREATE TRIGGER migration_fault BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT, 'scope-fixture-fault'); END`, table)); err != nil {
				t.Fatal(err)
			}
			if _, err := migrateLegacyRows(t.Context(), source, target, secret); err == nil || !strings.Contains(err.Error(), "scope-fixture-fault") {
				t.Fatalf("injected failure did not abort import: %v", err)
			}
			for _, imported := range []string{"api_tokens", "token_project_scopes", "connector_targets", "connector_credential_profiles", "connector_runtime_surfaces", "connector_credential_resources", "token_connector_action_permissions"} {
				assertCount(t, target, "SELECT COUNT(*) FROM "+imported, 0)
			}
			assertCount(t, source, `SELECT COUNT(*) FROM api_tokens WHERE id = 7`, 1)
			if _, err := target.Exec(`DROP TRIGGER migration_fault`); err != nil {
				t.Fatal(err)
			}
			if _, err := migrateLegacyRows(t.Context(), source, target, secret); err != nil {
				t.Fatalf("clean retry failed: %v", err)
			}
			assertCount(t, target, `SELECT COUNT(*) FROM token_project_scopes WHERE token_id = 7 AND enabled = 1`, 1)
		})
	}
}
