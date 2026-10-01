package transferruntime

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func newNativeDownloadBudgetFixture(t *testing.T, count int) (*Runner, *Runtime, filetransfer.BatchRecord, *sql.DB) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfers.aipdb"), "TransferBudgetPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtime := &Runtime{
		storageID: "budget-fixture", store: filetransfer.NewStore(database), jobs: &transferjobs.Registry{},
		finalization: transferjobs.NewFinalizationLifetime(), observe: func(context.Context, string, *int64, int64, string, any) {},
	}
	t.Cleanup(runtime.finalization.Stop)
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data.aipdb"), MaxObjectBytes: 8, MaxBatchBytes: 16, TempTTL: time.Hour})
	items := make([]filetransfer.CreateRequest, count)
	for index := range items {
		path, err := runner.ReserveDownloadTempFile(runtime)
		if err != nil {
			t.Fatal(err)
		}
		items[index] = filetransfer.CreateRequest{RemotePath: fmt.Sprintf("/object-%d", index), FileName: fmt.Sprintf("object-%d", index), TempPath: path}
	}
	batch, err := runtime.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: insertTransferRuntime(t, database), Direction: filetransfer.DirectionDownload,
		Source: filetransfer.SourceUI, Items: items,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, runtime, batch, database
}

func TestDownloadBatchBudgetBlocksGrowingFilesBeforeExtraDispatch(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 3)
	adapter := &byteBudgetAdapter{download: func(_ context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		if err := os.WriteFile(path, make([]byte, 8), 0o600); err != nil {
			return connectors.TransferResult{}, err
		}
		options.Progress(8, 8)
		return connectors.TransferResult{Bytes: 8}, nil
	}}
	runner.runBatch(t.Context(), runtime, batch.ID, false, Execution{Adapter: adapter, Boundary: actionresult.NewCredentialBoundary(nil)})
	stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindValidation || stored.TransferredBytes != 16 || stored.ArchivePath != "" {
		t.Fatalf("over-budget batch published: batch=%#v err=%v", stored, err)
	}
	if !reflect.DeepEqual(adapter.limits, []int64{8, 8}) {
		t.Fatalf("extra remote dispatch after exhaustion: %v", adapter.limits)
	}
	assertNativeDownloadTempsRemoved(t, batch)
}

func TestDownloadBatchBudgetIncludesPartialFailureBytes(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 3)
	adapter := &byteBudgetAdapter{}
	adapter.download = func(_ context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		bytes := int64(8)
		if len(adapter.limits) == 1 {
			bytes = 6
		} else if options.MaxBytes < bytes {
			return connectors.TransferResult{}, connectors.ErrTransferByteLimit
		}
		if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
			return connectors.TransferResult{}, err
		}
		options.Progress(4, bytes)
		if len(adapter.limits) == 1 {
			return connectors.TransferResult{Bytes: bytes}, errors.New("reader failed after partial write")
		}
		return connectors.TransferResult{Bytes: bytes}, nil
	}
	runner.runBatch(t.Context(), runtime, batch.ID, false, Execution{Adapter: adapter, Boundary: actionresult.NewCredentialBoundary(nil)})
	stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindValidation || stored.TransferredBytes != 14 || stored.ArchivePath != "" {
		t.Fatalf("partial bytes lost: batch=%#v err=%v", stored, err)
	}
	if !reflect.DeepEqual(adapter.limits, []int64{8, 8, 2}) || stored.Items[0].TransferredBytes != 6 {
		t.Fatalf("partial failure refunded: limits=%v batch=%#v", adapter.limits, stored)
	}
	assertNativeDownloadTempsRemoved(t, batch)
}

func TestDownloadBatchBudgetExactLimitPublishesBoundedArchive(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 2)
	adapter := &byteBudgetAdapter{download: func(_ context.Context, path string, _ connectors.TransferOptions) (connectors.TransferResult, error) {
		return connectors.TransferResult{Bytes: 8}, os.WriteFile(path, make([]byte, 8), 0o600)
	}}
	runner.runBatch(t.Context(), runtime, batch.ID, false, Execution{Adapter: adapter, Boundary: actionresult.NewCredentialBoundary(nil)})
	stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusCompleted || stored.TransferredBytes != 16 || stored.ArchivePath == "" {
		t.Fatalf("exact-limit batch rejected: batch=%#v err=%v", stored, err)
	}
	archive, err := zip.OpenReader(stored.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var bytes int64
	for _, item := range archive.File {
		reader, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		written, err := io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read archive: err=%v close=%v", err, closeErr)
		}
		bytes += written
	}
	if bytes != 16 || len(archive.File) != 2 {
		t.Fatalf("archive bytes=%d entries=%d", bytes, len(archive.File))
	}
}

func TestDownloadBatchCancellationKeepsActualBytesAndDoesNotArchive(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	adapter := &byteBudgetAdapter{download: func(ctx context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		if err := os.WriteFile(path, make([]byte, 6), 0o600); err != nil {
			return connectors.TransferResult{}, err
		}
		options.Progress(4, 8)
		cancel()
		return connectors.TransferResult{}, ctx.Err()
	}}
	runner.runBatch(ctx, runtime, batch.ID, false, Execution{Adapter: adapter, Boundary: actionresult.NewCredentialBoundary(nil)})
	stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusCanceled || stored.TransferredBytes != 6 || stored.ArchivePath != "" || len(adapter.limits) != 1 {
		t.Fatalf("canceled batch lost evidence or dispatched: batch=%#v calls=%v err=%v", stored, adapter.limits, err)
	}
	assertNativeDownloadTempsRemoved(t, batch)
}

func assertNativeDownloadTempsRemoved(t *testing.T, batch filetransfer.BatchRecord) {
	t.Helper()
	for _, item := range batch.Items {
		if _, err := os.Lstat(item.TempPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("terminal staging retained: path=%s err=%v", item.TempPath, err)
		}
	}
}
