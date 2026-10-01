package legacymigration

import (
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestLegacyMigrationPreservesAllPermissionRulesAfterReopen(t *testing.T) {
	for _, fixture := range []struct {
		name, rule, expiry string
		inactive           bool
	}{
		{"always", "always_run", "2030-01-01T00:00:00Z", false},
		{"prompt", "approval_required", "2030-01-01T00:00:00Z", false},
		{"blocked", "blocked", "", false},
		{"expired", "always_run", "2026-01-01T00:00:00Z", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source, target, path, secret := openLegacyTokenScopeFixture(t)
			const created = "2025-12-01T00:00:00Z"
			const updated = "2025-12-02T00:00:00Z"
			if _, err := source.Exec(`UPDATE token_server_permissions SET execution_rule = ?, expires_at = NULLIF(?, ''), created_at = ?, updated_at = ? WHERE token_id = 7`, fixture.rule, fixture.expiry, created, updated); err != nil {
				t.Fatal(err)
			}
			if _, err := migrateLegacyRows(t.Context(), source, target, secret); err != nil {
				t.Fatal(err)
			}
			if err := target.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := db.OpenEncrypted(path, "TargetScopePassword123")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var rule, expiry, storedCreated, storedUpdated string
			var targetID, profileID int64
			if err := reopened.QueryRow(`SELECT target_id, profile_id, execution_rule, COALESCE(expires_at, ''), created_at, updated_at FROM token_connector_action_permissions WHERE token_id = 7 AND action_name = 'exec'`).Scan(&targetID, &profileID, &rule, &expiry, &storedCreated, &storedUpdated); err != nil {
				t.Fatal(err)
			}
			if rule != fixture.rule || expiry != fixture.expiry || storedCreated != created || storedUpdated != updated {
				t.Fatalf("permission changed: %q %q %q %q", rule, expiry, storedCreated, storedUpdated)
			}
			permission, err := connectortargets.NewStore(reopened).GetActionPermission(t.Context(), 7, targetID, profileID, "exec", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
			if fixture.inactive {
				if !errors.Is(err, connectortargets.ErrActionPermissionNotFound) {
					t.Fatalf("expired permission became effective: %#v %v", permission, err)
				}
			} else if err != nil || string(permission.ExecutionRule) != fixture.rule || permission.ExpiresAt != fixture.expiry {
				t.Fatalf("wrong effective permission: %#v %v", permission, err)
			}
		})
	}
}
