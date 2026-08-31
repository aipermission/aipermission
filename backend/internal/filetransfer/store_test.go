package filetransfer

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreCreatesListsAndUpdatesFileTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	created, err := store.Create(ctx, CreateRequest{
		RuntimeID:  runtimeID,
		Direction:  DirectionUpload,
		Source:     SourceUI,
		LocalPath:  "deploy.tar.gz",
		RemotePath: "/tmp/deploy.tar.gz",
		FileName:   "deploy.tar.gz",
		SizeBytes:  2048,
		TempPath:   "/tmp/aipermission/staged",
	})
	if err != nil {
		t.Fatalf("create file transfer: %v", err)
	}
	if created.Status != StatusPending || created.Direction != DirectionUpload || created.TargetName != "worker-1" {
		t.Fatalf("unexpected created transfer: %#v", created)
	}
	if created.TempPath != "/tmp/aipermission/staged" {
		t.Fatalf("store should keep internal temp path for backend cleanup")
	}

	if ok, err := store.MarkRunning(ctx, created.ID); err != nil || !ok {
		t.Fatalf("mark running: %v", err)
	}
	if err := store.UpdateProgress(ctx, created.ID, 1024, 2048); err != nil {
		t.Fatalf("update progress: %v", err)
	}
	if ok, err := store.Complete(ctx, created.ID, 2048, "abc123"); err != nil || !ok {
		t.Fatalf("complete transfer: %v", err)
	}
	completed, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get completed transfer: %v", err)
	}
	if completed.Status != StatusCompleted || completed.TransferredBytes != 2048 || completed.ChecksumSHA256 != "abc123" || completed.CompletedAt == "" {
		t.Fatalf("unexpected completed transfer: %#v", completed)
	}

	items, total, err := store.List(ctx, ListFilter{Direction: DirectionUpload, Query: "deploy"})
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("unexpected transfer list: total=%d items=%#v", total, items)
	}

	if ok, err := store.Cancel(ctx, created.ID, "too late"); err != nil || ok {
		t.Fatalf("completed transfer should not be cancelable: ok=%v err=%v", ok, err)
	}
}

func TestStoreFinalizesPausedTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	createPaused := func(name string) Record {
		t.Helper()
		item, err := store.Create(ctx, CreateRequest{
			RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
			LocalPath: name, RemotePath: "/tmp/" + name, FileName: name,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if ok, err := store.MarkRunning(ctx, item.ID); err != nil || !ok {
			t.Fatalf("mark %s running: ok=%v err=%v", name, ok, err)
		}
		if ok, err := store.Pause(ctx, item.ID); err != nil || !ok {
			t.Fatalf("pause %s: ok=%v err=%v", name, ok, err)
		}
		return item
	}

	failed := createPaused("failed.txt")
	if ok, err := store.Fail(ctx, failed.ID, "timed out"); err != nil || !ok {
		t.Fatalf("fail paused transfer: ok=%v err=%v", ok, err)
	}
	canceled := createPaused("canceled.txt")
	if ok, err := store.Cancel(ctx, canceled.ID, "canceled by local user"); err != nil || !ok {
		t.Fatalf("cancel paused transfer: ok=%v err=%v", ok, err)
	}

	failed, err = store.Get(ctx, failed.ID)
	if err != nil || failed.Status != StatusFailed {
		t.Fatalf("failed paused transfer=%#v err=%v", failed, err)
	}
	canceled, err = store.Get(ctx, canceled.ID)
	if err != nil || canceled.Status != StatusCanceled {
		t.Fatalf("canceled paused transfer=%#v err=%v", canceled, err)
	}
}

func TestStorePersistsStructuredTransferFailureKinds(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	created, err := store.Create(context.Background(), CreateRequest{
		RuntimeID: insertTestServer(t, database), Direction: DirectionUpload, Source: SourceUI,
		LocalPath: "artifact.zip", RemotePath: "/tmp/artifact.zip", FileName: "artifact.zip",
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if ok, err := store.FailWithKind(context.Background(), created.ID, "remote outcome unknown", FailureKindOutcomeUnknown); err != nil || !ok {
		t.Fatalf("fail transfer: ok=%v err=%v", ok, err)
	}
	failed, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if failed.Status != StatusFailed || failed.FailureKind != FailureKindOutcomeUnknown {
		t.Fatalf("unexpected failed transfer: %#v", failed)
	}
	var preview string
	if err := database.QueryRow(`SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, created.ID).Scan(&preview); err != nil {
		t.Fatalf("read transfer history preview: %v", err)
	}
	if preview != `{"failure_kind":"outcome_unknown"}` {
		t.Fatalf("history preview=%s", preview)
	}
	var auditPayload string
	if err := database.QueryRow(`SELECT payload_json FROM audit_outbox WHERE action = 'file_transfer.failed' AND payload_json LIKE ? ORDER BY id DESC LIMIT 1`, fmt.Sprintf(`%%"transfer_id":%d,%%`, created.ID)).Scan(&auditPayload); err != nil {
		t.Fatalf("read transfer failure audit: %v", err)
	}
	if !strings.Contains(auditPayload, `"failure_kind":"outcome_unknown"`) {
		t.Fatalf("failure kind missing from audit payload: %s", auditPayload)
	}
}

func TestStoreAuditsTransferLifecycleWithoutProgressNoise(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	created, err := store.Create(ctx, CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		LocalPath: "deploy.tar.gz", RemotePath: "/tmp/deploy.tar.gz", FileName: "deploy.tar.gz",
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if ok, err := store.MarkRunning(ctx, created.ID); err != nil || !ok {
		t.Fatalf("mark transfer running: ok=%v err=%v", ok, err)
	}
	if err := store.UpdateProgress(ctx, created.ID, 512, 1024); err != nil {
		t.Fatalf("update transfer progress: %v", err)
	}
	if ok, err := store.Complete(ctx, created.ID, 1024, "abc123"); err != nil || !ok {
		t.Fatalf("complete transfer: ok=%v err=%v", ok, err)
	}

	rows, err := database.Query(`
		SELECT action FROM audit_outbox
		WHERE payload_json LIKE ?
		ORDER BY id`, fmt.Sprintf(`%%"transfer_id":%d,%%`, created.ID))
	if err != nil {
		t.Fatalf("query transfer audit events: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("scan transfer audit action: %v", err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate transfer audit actions: %v", err)
	}
	want := []string{"file_transfer.created", "file_transfer.running", "file_transfer.completed"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}
	for index := range want {
		if actions[index] != want[index] {
			t.Fatalf("audit actions = %v, want %v", actions, want)
		}
	}
}

func TestStoreRollsBackTransferWhenAuditOutboxIsUnavailable(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	if _, err := database.Exec(`DROP TABLE audit_outbox`); err != nil {
		t.Fatalf("drop audit outbox: %v", err)
	}

	_, err = NewStore(database).Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		LocalPath: "deploy.tar.gz", RemotePath: "/tmp/deploy.tar.gz", FileName: "deploy.tar.gz",
	})
	if err == nil {
		t.Fatal("create transfer should fail when its audit event cannot be appended")
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM file_transfers`).Scan(&count); err != nil {
		t.Fatalf("count file transfers: %v", err)
	}
	if count != 0 {
		t.Fatalf("file transfer mutation committed without audit event: count=%d", count)
	}
}

func TestStoreCreatesPausesAndCompletesBatches(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", SizeBytes: 100, TempPath: "/tmp/a"},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if batch.Status != StatusPending || batch.TotalItems != 2 || batch.SizeBytes != 300 || len(batch.Items) != 2 {
		t.Fatalf("unexpected batch: %#v", batch)
	}
	if batch.Items[0].BatchID != batch.ID || batch.Items[0].QueueIndex != 0 || batch.Items[1].QueueIndex != 1 {
		t.Fatalf("unexpected batch item ordering: %#v", batch.Items)
	}

	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark item running: ok=%v err=%v", ok, err)
	}
	if err := store.UpdateProgressStats(ctx, batch.Items[0].ID, 50, 100, 25, 2); err != nil {
		t.Fatalf("update item progress: %v", err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate batch: %v", err)
	}
	progress, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch progress: %v", err)
	}
	if progress.TransferredBytes != 50 || progress.BytesPerSecond != 25 || progress.ETASeconds < 0 {
		t.Fatalf("unexpected batch progress: %#v", progress)
	}

	if ok, err := store.PauseBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}
	paused, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get paused batch: %v", err)
	}
	if paused.Status != StatusPaused || paused.Items[0].Status != StatusPaused {
		t.Fatalf("unexpected paused batch: %#v", paused)
	}
	if ok, err := store.ResumeBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("resume batch: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[0].ID, 100, "aaa"); err != nil || !ok {
		t.Fatalf("complete first item: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[1].ID); err != nil || !ok {
		t.Fatalf("mark second item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[1].ID, 200, "bbb"); err != nil || !ok {
		t.Fatalf("complete second item: ok=%v err=%v", ok, err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate completed batch: %v", err)
	}
	if ok, err := store.CompleteBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("complete batch: ok=%v err=%v", ok, err)
	}
	completed, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get completed batch: %v", err)
	}
	if completed.Status != StatusCompleted || completed.CompletedItems != 2 || completed.TransferredBytes != 300 || completed.ETASeconds != 0 {
		t.Fatalf("unexpected completed batch: %#v", completed)
	}

	batches, total, err := store.ListBatches(ctx, BatchListFilter{Direction: DirectionUpload, TargetIDs: []int64{runtimeID}, Query: "worker"})
	if err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if total != 1 || len(batches) != 1 || batches[0].ID != batch.ID {
		t.Fatalf("unexpected batch list: total=%d items=%#v", total, batches)
	}
	batches, total, err = store.ListBatches(ctx, BatchListFilter{TargetIDs: []int64{runtimeID + 1000}})
	if err != nil {
		t.Fatalf("list filtered batches: %v", err)
	}
	if total != 0 || len(batches) != 0 {
		t.Fatalf("unexpected filtered batch list: total=%d items=%#v", total, batches)
	}
}

func TestStoreFailsBatchWithoutReclassifyingCompletedItems(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{
			{LocalPath: "done.txt", RemotePath: "/tmp/done.txt", FileName: "done.txt"},
			{LocalPath: "pending.txt", RemotePath: "/tmp/pending.txt", FileName: "pending.txt"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark first item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[0].ID, 10, "done"); err != nil || !ok {
		t.Fatalf("complete first item: ok=%v err=%v", ok, err)
	}

	if ok, err := store.FailBatchWithKind(ctx, batch.ID, "file transfer batch timed out", FailureKindTimeout); err != nil || !ok {
		t.Fatalf("fail batch: ok=%v err=%v", ok, err)
	}
	failed, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get failed batch: %v", err)
	}
	if failed.Status != StatusFailed || failed.Error != "file transfer batch timed out" || failed.FailureKind != FailureKindTimeout {
		t.Fatalf("unexpected failed batch: %#v", failed)
	}
	if failed.CompletedItems != 1 || failed.FailedItems != 1 {
		t.Fatalf("unexpected failed batch counters: %#v", failed)
	}
	if failed.Items[0].Status != StatusCompleted || failed.Items[1].Status != StatusFailed {
		t.Fatalf("unexpected failed batch items: %#v", failed.Items)
	}
	if failed.Items[1].Error != "file transfer batch timed out" || failed.Items[1].FailureKind != FailureKindTimeout {
		t.Fatalf("pending item timeout error=%q", failed.Items[1].Error)
	}
}

func TestStoreApprovesPendingTransferBatchItems(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeID:    runtimeID,
		Direction:    DirectionDownload,
		Source:       SourceMCP,
		Status:       StatusPendingApproval,
		ApprovalNote: "initial",
		Items: []CreateRequest{
			{RemotePath: "/tmp/a.log", FileName: "a.log", SizeBytes: 100, TempPath: "/tmp/a"},
			{RemotePath: "/tmp/b.log", FileName: "b.log", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create pending approval batch: %v", err)
	}
	if batch.Status != StatusPendingApproval || batch.Items[0].Status != StatusPendingApproval {
		t.Fatalf("unexpected pending approval batch: %#v", batch)
	}

	approved, rejected, err := store.ApproveBatch(ctx, batch.ID, BatchApprovalRequest{
		ApprovedItemIDs: []int64{batch.Items[0].ID},
		Note:            "skip b.log",
	})
	if err != nil {
		t.Fatalf("approve batch: %v", err)
	}
	if approved.Status != StatusPending || approved.ApprovalNote != "skip b.log" || len(rejected) != 1 || rejected[0].ID != batch.Items[1].ID {
		t.Fatalf("unexpected approved batch: batch=%#v rejected=%#v", approved, rejected)
	}
	if approved.Items[0].Status != StatusPending || approved.Items[1].Status != StatusCanceled || approved.Items[1].Error != "skip b.log" {
		t.Fatalf("unexpected approved items: %#v", approved.Items)
	}

	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark approved batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark approved item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[0].ID, 100, "checksum"); err != nil || !ok {
		t.Fatalf("complete approved item: ok=%v err=%v", ok, err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate partial batch: %v", err)
	}
	if ok, err := store.CompleteBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("complete partial batch: ok=%v err=%v", ok, err)
	}
	completed, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get completed partial batch: %v", err)
	}
	if completed.Status != StatusCompleted || completed.CompletedItems != 1 || completed.CanceledItems != 1 {
		t.Fatalf("unexpected completed partial batch: %#v", completed)
	}
}

func TestStoreUpdatesPendingBatchItemSizesAtomically(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	batch, err := store.CreateBatch(context.Background(), CreateBatchRequest{
		RuntimeID: insertTestServer(t, database),
		Direction: DirectionDownload,
		Source:    SourceMCP,
		Items: []CreateRequest{
			{RemotePath: "/tmp/a.log", FileName: "a.log", TempPath: "/tmp/a"},
			{RemotePath: "/tmp/b.log", FileName: "b.log", TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if err := store.UpdatePendingBatchItemSizes(context.Background(), batch.ID, map[int64]int64{
		batch.Items[0].ID: 100,
		batch.Items[1].ID: 200,
	}); err != nil {
		t.Fatalf("update pending sizes: %v", err)
	}
	updated, err := store.GetBatch(context.Background(), batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if updated.SizeBytes != 300 || updated.Items[0].SizeBytes != 100 || updated.Items[1].SizeBytes != 200 {
		t.Fatalf("unexpected updated sizes: %#v", updated)
	}
}

func TestStoreUpdatesPausedBatchQueue(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", SizeBytes: 100, TempPath: "/tmp/a"},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
			{LocalPath: "c.txt", RemotePath: "/tmp/c.txt", FileName: "c.txt", SizeBytes: 300, TempPath: "/tmp/c"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark first item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.PauseBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}

	removed, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[2].ID})
	if err != nil {
		t.Fatalf("update paused queue: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != batch.Items[1].ID || removed[0].TempPath != "/tmp/b" {
		t.Fatalf("unexpected removed items: %#v", removed)
	}
	if _, err := store.Get(ctx, batch.Items[1].ID); err != ErrNotFound {
		t.Fatalf("removed pending item should be deleted, got err=%v", err)
	}
	updated, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get updated batch: %v", err)
	}
	if updated.TotalItems != 2 || updated.SizeBytes != 400 {
		t.Fatalf("unexpected recalculated batch: %#v", updated)
	}
	if updated.Items[0].ID != batch.Items[0].ID || updated.Items[0].Status != StatusPaused {
		t.Fatalf("paused running item should stay in place: %#v", updated.Items)
	}
	if updated.Items[1].ID != batch.Items[2].ID || updated.Items[1].Status != StatusPending {
		t.Fatalf("remaining pending item should stay queued: %#v", updated.Items)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[0].ID}); err != ErrInvalidState {
		t.Fatalf("non-pending items must not be editable, got err=%v", err)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{0}); err != ErrInvalidArgument {
		t.Fatalf("non-positive item ids should fail as invalid arguments, got err=%v", err)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[2].ID, batch.Items[2].ID}); err != ErrInvalidArgument {
		t.Fatalf("duplicate item ids should fail as invalid arguments, got err=%v", err)
	}
}

func TestStoreFailsActiveTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	standalone, err := store.Create(ctx, CreateRequest{
		RuntimeID:  runtimeID,
		Direction:  DirectionDownload,
		Source:     SourceUI,
		RemotePath: "/tmp/a.txt",
		FileName:   "a.txt",
		TempPath:   "/tmp/a",
	})
	if err != nil {
		t.Fatalf("create standalone transfer: %v", err)
	}
	if ok, err := store.MarkRunning(ctx, standalone.ID); err != nil || !ok {
		t.Fatalf("mark standalone running: ok=%v err=%v", ok, err)
	}
	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark batch item running: ok=%v err=%v", ok, err)
	}

	if err := store.FailActive(ctx, "transfer stopped", "batch stopped"); err != nil {
		t.Fatalf("fail active transfers: %v", err)
	}
	updated, err := store.Get(ctx, standalone.ID)
	if err != nil {
		t.Fatalf("get standalone: %v", err)
	}
	if updated.Status != StatusFailed || updated.Error != "transfer stopped" || updated.CompletedAt == "" {
		t.Fatalf("unexpected failed standalone transfer: %#v", updated)
	}
	updatedBatch, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if updatedBatch.Status != StatusFailed || updatedBatch.Error != "batch stopped" || updatedBatch.Items[0].Status != StatusFailed {
		t.Fatalf("unexpected failed batch: %#v", updatedBatch)
	}
}

func TestStoreValidatesFileTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := NewStore(database)

	if _, err := store.Create(context.Background(), CreateRequest{Direction: DirectionUpload, RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("missing runtime_id should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{RuntimeID: 1, Direction: "copy", RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("invalid direction should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{RuntimeID: 1, Direction: DirectionDownload, RemotePath: "bad\npath"}); err == nil {
		t.Fatalf("control characters should fail")
	}
}

func insertTestServer(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	targetResult, err := database.Exec(
		`INSERT INTO connector_targets (project_id, connector_kind, name, config_json, created_at, updated_at)
			VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active'), 'ssh', ?, ?, datetime('now'), datetime('now'))`,
		"worker-1",
		`{"host":"127.0.0.1","port":22}`,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	profileResult, err := database.Exec(
		`INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, 'ssh', 'private_key', 'root', '{"username":"root","ssh_key_id":1}', 'encrypted', datetime('now'), datetime('now'))`,
		targetID,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := profileResult.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}
	surfaceResult, err := database.Exec(
		`INSERT INTO connector_runtime_surfaces (
			connector_kind, target_id, profile_id, capability_kind, label, created_at, updated_at
		)
		VALUES ('ssh', ?, ?, 'live_console', 'root terminal', datetime('now'), datetime('now'))`,
		targetID,
		profileID,
	)
	if err != nil {
		t.Fatalf("insert runtime surface: %v", err)
	}
	runtimeID, err := surfaceResult.LastInsertId()
	if err != nil {
		t.Fatalf("runtime surface id: %v", err)
	}
	return runtimeID
}
