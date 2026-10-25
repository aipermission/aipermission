package observation

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func TestReadTriggeredVaultExpiryAndAuditCommitAtomically(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "expiry.db"), "FixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	project, err := projects.NewStore(database).Create(t.Context(), "Expiry fixture")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	store := New().VaultRequestStore(t.Context(), Runtime{Database: database})
	request, _, err := store.Create(t.Context(), vaultrequests.CreateInput{
		TokenID: token.ID, ProjectID: project.ID, ActionName: vaultrequests.ActionGenerateItem,
		IdempotencyKey: "expiry-audit", Reason: "verify atomic expiration", ApprovalContextHash: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE vault_action_requests SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_expiry_audit BEFORE INSERT ON audit_outbox
 WHEN NEW.action = 'vault.action_request.expired' BEGIN SELECT RAISE(ABORT, 'fixture audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), request.ID); err == nil {
		t.Fatal("expiry ignored required audit failure")
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM vault_action_requests WHERE id = ?`, request.ID).Scan(&status); err != nil || status != vaultrequests.StatusApprovalPending {
		t.Fatalf("expiry escaped audit rollback: %s %v", status, err)
	}
	if _, err := database.Exec(`DROP TRIGGER fail_expiry_audit`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := store.Get(t.Context(), request.ID)
		if err != nil || result.Status != vaultrequests.StatusExpired {
			t.Fatalf("read did not persist expiry: %v", err)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_outbox WHERE action = 'vault.action_request.expired'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expiry audit count=%d err=%v", count, err)
	}
}
