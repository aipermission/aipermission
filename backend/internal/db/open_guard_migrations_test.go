package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenEncryptedRejectsSymlinkDatabasePath(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.db")
	database, err := OpenEncrypted(target, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenEncrypted(link, "correct-password"); err == nil {
		_ = opened.Close()
		t.Fatal("symlinked database path was opened")
	}
}

func TestVaultActionEnvelopeMigrationScrubsLegacyPublicMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-action-envelope.aipdb")
	password := "VaultActionEnvelopePassword123"
	database, err := OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{
		"guard_vault_action_request_envelope_insert",
		"guard_vault_action_request_envelope_update",
		"protect_vault_action_request_envelope_update",
	} {
		if _, err := database.Exec(`DROP TRIGGER IF EXISTS ` + trigger); err != nil {
			t.Fatalf("drop %s: %v", trigger, err)
		}
	}
	if _, err := database.Exec(`ALTER TABLE vault_action_requests DROP COLUMN encrypted_payload_json`); err != nil {
		t.Fatalf("remove envelope column: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM schema_migrations WHERE version = 33`); err != nil {
		t.Fatal(err)
	}
	projectResult, err := database.Exec(`
		INSERT INTO projects (name, slug, status, created_at, updated_at)
		VALUES ('Legacy Vault', 'legacy-vault', 'active', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := projectResult.LastInsertId()
	tokenResult, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, token_value, created_at, updated_at)
		VALUES ('legacy-vault-token', 'legacy-vault-hash', 'aip_legacy_vault', '', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	tokenID, _ := tokenResult.LastInsertId()
	requestResult, err := database.Exec(`
		INSERT INTO vault_action_requests (
			token_id, project_id, action_name, input_json, reason, status,
			approval_context_json, idempotency_key, created_at, expires_at, updated_at
		) VALUES (?, ?, 'generate_item', '{"usage_notes":[{"notes":"legacy-canary"}]}',
			'legacy-canary reason', 'approval_pending', '{}', 'legacy-envelope-request',
			datetime('now'), datetime('now', '+15 minutes'), datetime('now'))`, tokenID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := requestResult.LastInsertId()
	if _, err := database.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id,
			project_id, source, status, action_name, title, summary, input_json,
			created_at, updated_at
		) VALUES ('vault_action_request', ?, 'vault', 'vault', ?, ?, 'mcp',
			'pending_approval', 'generate_item', 'generate_item', 'legacy-canary reason',
			'{"usage_notes":[{"notes":"legacy-canary"}]}', datetime('now'), datetime('now'))`, requestID, tokenID, projectID); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var status, inputJSON, reason, encrypted string
	if err := database.QueryRow(`
		SELECT status, input_json, reason, encrypted_payload_json
		FROM vault_action_requests WHERE id = ?`, requestID,
	).Scan(&status, &inputJSON, &reason, &encrypted); err != nil {
		t.Fatal(err)
	}
	if status != "stale" || inputJSON != "{}" || reason != "[REDACTED LEGACY METADATA]" || encrypted != "" {
		t.Fatalf("migrated request status=%q input=%q reason=%q encrypted=%q", status, inputJSON, reason, encrypted)
	}
	var historyStatus, historyInput, historySummary string
	if err := database.QueryRow(`SELECT status, input_json, summary FROM history_entries WHERE source_ref_id = ?`, requestID).
		Scan(&historyStatus, &historyInput, &historySummary); err != nil {
		t.Fatal(err)
	}
	if historyStatus != "stale" || historyInput != "{}" || historySummary != "[REDACTED LEGACY METADATA]" {
		t.Fatalf("migrated history status=%q input=%q summary=%q", historyStatus, historyInput, historySummary)
	}
}

func TestOpenEncryptedDefersRunningProfileRestoreToAuditedRuntimeRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore-restart.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	if _, err := database.Exec(`
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, created_at, updated_at
		) VALUES ('restore-restart', 'identity', ?, ?, 'postgres', 'restore.sql', 'sha256', 9, 'running', datetime('now'), datetime('now'))`,
		targetID, profileID); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var status, errorCode string
	if err := reopened.QueryRow(`SELECT status, error_code FROM profile_restore_operations WHERE idempotency_key = 'restore-restart'`).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "running" || errorCode != "" {
		t.Fatalf("restore after restart status=%q error_code=%q", status, errorCode)
	}
}
