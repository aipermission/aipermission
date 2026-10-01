package transferruntime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestExpiredDownloadCleanupWaitsForActiveOwnerEvidence(t *testing.T) {
	for _, owner := range []string{"file", "batch"} {
		t.Run(owner, func(t *testing.T) {
			runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 1)
			item := batch.Items[0]
			if err := os.WriteFile(item.TempPath, make([]byte, 6), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.store.CancelBatch(t.Context(), batch.ID, "canceled"); err != nil {
				t.Fatal(err)
			}
			if err := runtime.store.SetTempExpiry(t.Context(), item.ID, item.TempPath, time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			_, cancel := context.WithCancel(t.Context())
			defer cancel()
			group, id := &runtime.jobs.Files, item.ID
			if owner == "batch" {
				group, id = &runtime.jobs.Batches, batch.ID
			}
			group.RegisterCancel(id, cancel)
			if err := runner.RecoverTempCleanup(t.Context(), runtime); err != nil {
				t.Fatal(err)
			}
			// Timer-triggered cleanup must obey the same ownership gate.
			runner.removeExpiredTransferTemp(runtime, item.ID, item.TempPath)
			if bytes, err := os.ReadFile(item.TempPath); err != nil || len(bytes) != 6 {
				t.Fatalf("active evidence deleted: bytes=%d err=%v", len(bytes), err)
			}
			group.UnregisterCancel(id)
			if err := runner.RecoverTempCleanup(t.Context(), runtime); err != nil {
				t.Fatal(err)
			}
			assertNativeDownloadTempsRemoved(t, batch)
			stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
			if err != nil || stored.Status != filetransfer.StatusCanceled || stored.TransferredBytes != 6 || stored.Items[0].TempPath != "" {
				t.Fatalf("recovered evidence not durable: batch=%#v err=%v", stored, err)
			}
		})
	}
}

func TestExpiredDownloadCleanupRetainsStagingUntilHistoryIsDurable(t *testing.T) {
	runner, runtime, batch, database := newNativeDownloadBudgetFixture(t, 1)
	item := batch.Items[0]
	if err := os.WriteFile(item.TempPath, make([]byte, 6), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.store.CancelBatch(t.Context(), batch.ID, "canceled"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_cleanup_evidence BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected evidence projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	runner.removeExpiredTransferTemp(runtime, item.ID, item.TempPath)
	if bytes, err := os.ReadFile(item.TempPath); err != nil || len(bytes) != 6 {
		t.Fatalf("undurable evidence deleted: bytes=%d err=%v", len(bytes), err)
	}
	stored, err := runtime.store.Get(t.Context(), item.ID)
	if err != nil || stored.TransferredBytes != 0 || stored.TempPath != item.TempPath {
		t.Fatalf("failed evidence update partially committed: item=%#v err=%v", stored, err)
	}
	if _, err := database.Exec(`DROP TRIGGER reject_cleanup_evidence`); err != nil {
		t.Fatal(err)
	}
	runner.removeExpiredTransferTemp(runtime, item.ID, item.TempPath)
	assertNativeDownloadTempsRemoved(t, batch)
	stored, err = runtime.store.Get(t.Context(), item.ID)
	if err != nil || stored.TransferredBytes != 6 || stored.TempPath != "" {
		t.Fatalf("retried evidence not durable: item=%#v err=%v", stored, err)
	}
}

func TestTransferOwnerActiveSeparatesFileAndBatchNamespaces(t *testing.T) {
	runtime := &Runtime{jobs: &transferjobs.Registry{}}
	_, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime.jobs.Batches.RegisterCancel(7, cancel)
	if transferOwnerActive(runtime, filetransfer.Record{ID: 7}) {
		t.Fatal("unrelated batch ID blocked file cleanup")
	}
	if !transferOwnerActive(runtime, filetransfer.Record{ID: 2, BatchID: 7}) {
		t.Fatal("active batch owner was ignored")
	}
	runtime.jobs.Batches.UnregisterCancel(7)
	runtime.jobs.Files.RegisterCancel(7, cancel)
	if !transferOwnerActive(runtime, filetransfer.Record{ID: 7}) || transferOwnerActive(runtime, filetransfer.Record{ID: 2, BatchID: 7}) {
		t.Fatal("file and batch ownership mixed")
	}
}
