package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestUploadRunnerKeepsStagingWhenClaimFails(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected connector I/O: %s %s", r.Method, r.URL.Path)
	}))
	defer remote.Close()
	runtimeID := createS3IdentityRuntime(t, fixture.server, remote.URL).TransferRuntimeID
	staged, _, _, err := handlers.stageUploadFile(strings.NewReader("owned staging"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: staged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_runner_history_projection BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	handlers.runUpload(context.Background(), runtime, item.ID, false)
	assertRunnerTransferAndStaging(t, runtime, item.ID, staged, filetransfer.StatusPending)
}

func TestUploadRunnerWithoutClaimDoesNotDeleteOwnedStaging(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected connector I/O: %s %s", r.Method, r.URL.Path)
	}))
	defer remote.Close()
	runtimeID := createS3IdentityRuntime(t, fixture.server, remote.URL).TransferRuntimeID
	staged, _, _, err := handlers.stageUploadFile(strings.NewReader("owned staging"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: staged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkRunning(context.Background(), item.ID); err != nil || !changed {
		t.Fatalf("claim upload: changed=%v err=%v", changed, err)
	}

	handlers.runUpload(context.Background(), runtime, item.ID, false)
	assertRunnerTransferAndStaging(t, runtime, item.ID, staged, filetransfer.StatusRunning)
}

func TestDownloadRunnerKeepsReservationWhenClaimFails(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected connector I/O: %s %s", r.Method, r.URL.Path)
	}))
	defer remote.Close()
	runtimeID := createS3IdentityRuntime(t, fixture.server, remote.URL).TransferRuntimeID
	reserved, err := handlers.reserveDownloadTempFile()
	if err != nil {
		t.Fatal(err)
	}
	item, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: reserved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_download_runner_history_projection BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	handlers.runDownload(context.Background(), runtime, item.ID)
	assertRunnerTransferAndStaging(t, runtime, item.ID, reserved, filetransfer.StatusPending)
}

func TestBatchArchivePersistenceFailureCannotFinalizeAsCompleted(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer remote.Close()
	runtimeID := createS3IdentityRuntime(t, fixture.server, remote.URL).TransferRuntimeID
	batch, err := runtime.fileTransfers.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{
			{RemotePath: "/one", FileName: "one", TempPath: filepath.Join(t.TempDir(), "one")},
			{RemotePath: "/two", FileName: "two", TempPath: filepath.Join(t.TempDir(), "two")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
	}
	archivePath := filepath.Join(t.TempDir(), "download.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_runner_archive BEFORE UPDATE OF archive_path ON file_transfer_batches
		BEGIN SELECT RAISE(ABORT, 'injected archive persistence failure'); END`); err != nil {
		t.Fatal(err)
	}

	if handlers.persistDownloadBatchArchive(t.Context(), runtime, batch, archivePath) {
		t.Fatal("archive persistence failure reported success")
	}
	stored, err := runtime.fileTransfers.GetBatch(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindLocalPersistence || stored.ArchivePath != "" {
		t.Fatalf("batch after archive persistence failure = %#v", stored)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive was removed before delayed cleanup: %v", err)
	}
}

func TestBatchArchiveRetainedWhenFailureStateCannotPersist(t *testing.T) {
	fixture := newAPITestFixture(t)
	runtime := fixture.server.activeRuntime()
	handlers := fileTransferHandlers{fixture.server}
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer remote.Close()
	runtimeID := createS3IdentityRuntime(t, fixture.server, remote.URL).TransferRuntimeID
	batch, err := runtime.fileTransfers.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{{RemotePath: "/one", FileName: "one", TempPath: filepath.Join(t.TempDir(), "one")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
	}
	archivePath := filepath.Join(t.TempDir(), "download.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_runner_history_projection BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() { result <- handlers.persistDownloadBatchArchive(ctx, runtime, batch, archivePath) }()
	time.Sleep(3 * fileTransferPersistenceRetryInterval)
	select {
	case <-result:
		t.Fatal("archive persistence runner exited before terminal state was durable")
	default:
	}
	stored, err := runtime.fileTransfers.GetBatch(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusRunning || stored.ArchivePath != "" {
		t.Fatalf("unpersisted failure changed batch = %#v", stored)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("recoverable archive was removed: %v", err)
	}
	if _, err := fixture.db.Exec(`DROP TRIGGER reject_runner_history_projection`); err != nil {
		t.Fatal(err)
	}
	select {
	case succeeded := <-result:
		if succeeded {
			t.Fatal("archive persistence failure reported success")
		}
	case <-ctx.Done():
		t.Fatal("archive persistence runner did not recover after storage became writable")
	}
	stored, err = runtime.fileTransfers.GetBatch(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindLocalPersistence {
		t.Fatalf("recovered batch = %#v", stored)
	}
}

func TestFileTransferLaunchableStatuses(t *testing.T) {
	for _, status := range []string{filetransfer.StatusPending, filetransfer.StatusPaused} {
		if !fileTransferCanLaunch(status) {
			t.Fatalf("status %q should be launchable", status)
		}
	}
	for _, status := range []string{
		filetransfer.StatusPendingApproval, filetransfer.StatusRunning, filetransfer.StatusCompleted,
		filetransfer.StatusFailed, filetransfer.StatusCanceled,
	} {
		if fileTransferCanLaunch(status) {
			t.Fatalf("status %q should not be launchable", status)
		}
	}
}

func assertRunnerTransferAndStaging(t *testing.T, runtime *databaseRuntime, id int64, staged, wantStatus string) {
	t.Helper()
	item, err := runtime.fileTransfers.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != wantStatus {
		t.Fatalf("transfer status=%q want=%q", item.Status, wantStatus)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("owned staging file was removed: %v", err)
	}
}
