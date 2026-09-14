package sqlstore

import (
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestStoreReadsWritesAndPurgesRetentionData(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "retention.db"), "RetentionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := Store{}
	if err := store.WriteSetting(t.Context(), database, "retention_history_days", "7", "2026-09-13T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	settings, err := store.ReadSettings(t.Context(), database, []string{"retention_history_days", "missing"})
	if err != nil || settings["retention_history_days"] != "7" {
		t.Fatalf("settings = %#v, err=%v", settings, err)
	}
	for name, purge := range map[string]func() (int64, error){
		"history":     func() (int64, error) { return store.PurgeHistory(t.Context(), database, "-7 days") },
		"audit":       func() (int64, error) { return store.PurgeAudit(t.Context(), database, "-7 days") },
		"console":     func() (int64, error) { return store.PurgeConsole(t.Context(), database, "-7 days") },
		"messages":    func() (int64, error) { return store.PurgeMessages(t.Context(), database, "-7 days") },
		"idempotency": func() (int64, error) { return store.PurgeExpiredIdempotency(t.Context(), database) },
	} {
		if _, err := purge(); err != nil {
			t.Fatalf("purge %s: %v", name, err)
		}
	}
}

func TestFileTransferRetentionPreservesPendingRemoteCleanup(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "retention.db"), "RetentionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := t.Context()
	targetStore := connectortargets.NewStore(database)
	target, err := targetStore.CreateTarget(ctx, connectortargets.CreateTargetInput{ConnectorKind: "test", Name: "Retention target", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targetStore.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := targetStore.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
		ConnectorKind: "test", TargetID: target.ID, ProfileID: profile.ID,
		CapabilityKind: connectortargets.RuntimeCapabilityFileTransfer,
	})
	if err != nil {
		t.Fatal(err)
	}
	transfers := filetransfer.NewStore(database)
	create := func(remote string) filetransfer.Record {
		item, err := transfers.Create(ctx, filetransfer.CreateRequest{
			RuntimeID: surface.ID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
			RemotePath: remote, FileName: "fixture",
		})
		if err != nil {
			t.Fatal(err)
		}
		if changed, err := transfers.MarkRunning(ctx, item.ID); err != nil || !changed {
			t.Fatalf("mark transfer running: changed=%v err=%v", changed, err)
		}
		return item
	}
	pendingCleanup := create("/pending-cleanup")
	if err := transfers.SetRemoteStagingRef(ctx, pendingCleanup.ID, "opaque-ref"); err != nil {
		t.Fatal(err)
	}
	if changed, err := transfers.FailWithKind(ctx, pendingCleanup.ID, "failed", filetransfer.FailureKindOutcomeUnknown); err != nil || !changed {
		t.Fatalf("fail pending cleanup transfer: changed=%v err=%v", changed, err)
	}
	purgeable := create("/purgeable")
	if changed, err := transfers.FailWithKind(ctx, purgeable.ID, "failed", filetransfer.FailureKindUnknown); err != nil || !changed {
		t.Fatalf("fail purgeable transfer: changed=%v err=%v", changed, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE file_transfers SET completed_at = datetime('now', '-10 days') WHERE id IN (?, ?)`, pendingCleanup.ID, purgeable.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := purgeFileTransfersWithoutPerRowAudit(ctx, database, "-7 days")
	if err != nil || deleted != 1 {
		t.Fatalf("retention deleted=%d err=%v", deleted, err)
	}
	if _, err := transfers.Get(ctx, pendingCleanup.ID); err != nil {
		t.Fatalf("retention discarded pending cleanup evidence: %v", err)
	}
	if _, err := transfers.Get(ctx, purgeable.ID); err == nil {
		t.Fatal("retention kept an ordinary expired transfer")
	}
}

func TestProfileRestoreRetentionLeavesBoundedIdempotencyTombstone(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "retention.db"), "RetentionPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := t.Context()
	targetStore := connectortargets.NewStore(database)
	target, err := targetStore.CreateTarget(ctx, connectortargets.CreateTargetInput{ConnectorKind: "test", Name: "Restore target", Config: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targetStore.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "test", Kind: "test", Label: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, created_at, updated_at, completed_at
		) VALUES ('restore-key', 'identity', ?, ?, 'test', 'restore.sql', 'hash', 10,
			'completed', datetime('now', '-10 days'), datetime('now', '-10 days'), datetime('now', '-10 days'))`, target.ID, profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO profile_restore_operations (
			idempotency_key, identity_hash, target_id, profile_id, connector_kind,
			filename, artifact_sha256, size_bytes, status, error_code, audit_pending,
			created_at, updated_at, completed_at
		) VALUES ('pending-audit-key', 'pending-identity', ?, ?, 'test', 'pending.sql', 'pending-hash', 10,
			'outcome_unknown', 'audit_persistence_failed', 1,
			datetime('now', '-10 days'), datetime('now', '-10 days'), datetime('now', '-10 days'))`, target.ID, profile.ID); err != nil {
		t.Fatal(err)
	}
	store := Store{}
	if _, err := store.PurgeHistory(ctx, database, "-7 days"); err != nil {
		t.Fatal(err)
	}
	var operations, tombstones int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile_restore_operations`).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile_restore_idempotency_tombstones WHERE idempotency_key = 'restore-key'`).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if operations != 1 || tombstones != 1 {
		t.Fatalf("restore retention operations=%d tombstones=%d", operations, tombstones)
	}
	var pendingAudit bool
	if err := database.QueryRowContext(ctx, `SELECT audit_pending FROM profile_restore_operations WHERE idempotency_key = 'pending-audit-key'`).Scan(&pendingAudit); err != nil || !pendingAudit {
		t.Fatalf("pending restore audit was not retained: pending=%v err=%v", pendingAudit, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE profile_restore_idempotency_tombstones SET expires_at = datetime('now', '-1 minute')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PurgeExpiredIdempotency(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile_restore_idempotency_tombstones`).Scan(&tombstones); err != nil || tombstones != 0 {
		t.Fatalf("expired restore tombstones=%d err=%v", tombstones, err)
	}
}
