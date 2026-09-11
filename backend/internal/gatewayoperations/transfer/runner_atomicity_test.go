package gatewaytransfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
)

type revisionGateTransferAdapter struct {
	rejectingTransferAdapter
	started     chan struct{}
	proceed     chan struct{}
	remoteCalls atomic.Int32
}

func (adapter *revisionGateTransferAdapter) DownloadFile(ctx context.Context, _ connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, _ string, _ string, _ connectorapi.TransferOptions) (connectorapi.TransferResult, error) {
	close(adapter.started)
	select {
	case <-ctx.Done():
		return connectorapi.TransferResult{}, ctx.Err()
	case <-adapter.proceed:
	}
	if _, _, _, err := runtime.TargetProfileByRuntimeID(ctx, runtimeID); err != nil {
		return connectorapi.TransferResult{}, err
	}
	adapter.remoteCalls.Add(1)
	return connectorapi.TransferResult{}, errors.New("remote operation should not start after profile drift")
}

func TestAuthorizedBatchLaunchRejectsAnotherConnectorRuntime(t *testing.T) {
	fixture := newTransferTestFixture(t)
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceMCP,
		Items: []filetransfer.CreateRequest{{RemotePath: "/report", FileName: "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.handlers.runner.LaunchBatch(t.Context(), fixture.runtime, batch.ID, false, transferapp.Execution{RuntimeID: fixture.runtimeID + 1}); !errors.Is(err, transferapp.ErrExecutionStale) {
		t.Fatalf("cross-connector launch error = %v", err)
	}
	stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusPending {
		t.Fatalf("unauthorized batch = %#v, %v", stored, err)
	}
}

func TestAcceptedBatchRejectsRuntimeDriftBeforeRemoteIO(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, transferTestFixture)
	}{
		{name: "target", mutate: func(t *testing.T, fixture transferTestFixture) {
			t.Helper()
			execution := fixture.execution(t)
			_, err := fixture.database.ExecContext(t.Context(), `UPDATE connector_targets SET name = 'rotated', updated_at = '2099-01-01T00:00:00Z' WHERE id = ?`, execution.target.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "profile", mutate: func(t *testing.T, fixture transferTestFixture) {
			t.Helper()
			execution := fixture.execution(t)
			_, err := fixture.database.ExecContext(t.Context(), `UPDATE connector_credential_profiles SET label = 'rotated', updated_at = '2099-01-01T00:00:00Z' WHERE id = ?`, execution.profile.ID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "surface", mutate: func(t *testing.T, fixture transferTestFixture) {
			t.Helper()
			_, err := fixture.database.ExecContext(t.Context(), `UPDATE connector_runtime_surfaces SET label = 'rotated', updated_at = '2099-01-01T00:00:00Z' WHERE id = ?`, fixture.runtimeID)
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &revisionGateTransferAdapter{started: make(chan struct{}), proceed: make(chan struct{})}
			fixture := newTransferTestFixtureWithAdapter(t, adapter)
			batch, err := fixture.handlers.CreateAndLaunchDownloadBatch(t.Context(), fixture.runtime, fixture.authorization(t), fixture.runtimeID, []string{"/report"}, "", filetransfer.SourceMCP)
			if err != nil {
				t.Fatalf("create and launch batch: %v", err)
			}
			select {
			case <-adapter.started:
			case <-time.After(time.Second):
				t.Fatal("transfer runner did not reach credential gate")
			}
			testCase.mutate(t, fixture)
			close(adapter.proceed)
			waitCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if !fixture.jobs.Wait(waitCtx) {
				t.Fatal("drifted transfer did not settle")
			}
			stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
			if err != nil || stored.Status != filetransfer.StatusFailed {
				t.Fatalf("drifted batch = %#v, %v", stored, err)
			}
			if adapter.remoteCalls.Load() != 0 {
				t.Fatalf("remote calls after %s drift = %d", testCase.name, adapter.remoteCalls.Load())
			}
		})
	}
}

func TestCreateAndLaunchDownloadBatchTerminalizesRejectedLaunch(t *testing.T) {
	fixture := newTransferTestFixture(t)
	authorization := fixture.authorization(t)
	fixture.jobs.Close()
	_, err := fixture.handlers.CreateAndLaunchDownloadBatch(t.Context(), fixture.runtime, authorization, fixture.runtimeID, []string{"/report"}, "", filetransfer.SourceMCP)
	if !errors.Is(err, transferapp.ErrRuntimeClosing) {
		t.Fatalf("closed registry launch error = %v", err)
	}
	batches, total, err := fixture.store.ListBatches(t.Context(), filetransfer.BatchListFilter{Limit: 10})
	if err != nil || total != 1 || len(batches) != 1 {
		t.Fatalf("rejected batches = %#v total=%d err=%v", batches, total, err)
	}
	stored, err := fixture.store.GetBatch(t.Context(), batches[0].ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindInterrupted {
		t.Fatalf("rejected batch = %#v, %v", stored, err)
	}
	for _, item := range stored.Items {
		if item.TempPath != "" {
			if _, statErr := os.Stat(item.TempPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected batch temp remains at %q: %v", item.TempPath, statErr)
			}
		}
	}
}

func TestIdempotentBatchReplayDoesNotRejectAlreadyRunningJob(t *testing.T) {
	adapter := &revisionGateTransferAdapter{started: make(chan struct{}), proceed: make(chan struct{})}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	batch, err := fixture.handlers.CreateAndLaunchDownloadBatch(
		t.Context(), fixture.runtime, fixture.authorization(t), fixture.runtimeID,
		[]string{"/report"}, "", filetransfer.SourceMCP,
	)
	if err != nil {
		t.Fatalf("create and launch batch: %v", err)
	}
	select {
	case <-adapter.started:
	case <-time.After(time.Second):
		t.Fatal("first batch runner did not start")
	}
	if err := fixture.handlers.runner.LaunchBatch(t.Context(), fixture.runtime, batch.ID, false, fixture.execution(t).runnerExecution()); err != nil {
		t.Fatalf("idempotent running replay was rejected: %v", err)
	}
	stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status == filetransfer.StatusFailed {
		t.Fatalf("running replay terminalized active batch = %#v, %v", stored, err)
	}
	close(adapter.proceed)
	if !fixture.jobs.Wait(t.Context()) {
		t.Fatal("batch runner did not drain")
	}
}

func TestCreateAndLaunchDownloadBatchRejectsStaleAuthorizationBeforePersistence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*connectorapi.TransferAuthorization)
	}{
		{name: "connector kind", mutate: func(value *connectorapi.TransferAuthorization) { value.ConnectorKind = "other" }},
		{name: "target id", mutate: func(value *connectorapi.TransferAuthorization) { value.TargetID++ }},
		{name: "target ref", mutate: func(value *connectorapi.TransferAuthorization) { value.TargetRef = "other" }},
		{name: "target revision", mutate: func(value *connectorapi.TransferAuthorization) { value.TargetUpdatedAt = "stale" }},
		{name: "profile id", mutate: func(value *connectorapi.TransferAuthorization) { value.ProfileID++ }},
		{name: "profile revision", mutate: func(value *connectorapi.TransferAuthorization) { value.ProfileUpdatedAt = "stale" }},
		{name: "secret revision", mutate: func(value *connectorapi.TransferAuthorization) { value.ProfileSecretRevision = "stale" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			authorization := fixture.authorization(t)
			testCase.mutate(&authorization)

			_, err := fixture.handlers.CreateAndLaunchDownloadBatch(t.Context(), fixture.runtime, authorization, fixture.runtimeID, []string{"/report"}, "", filetransfer.SourceMCP)
			if !errors.Is(err, errTransferExecutionStale) {
				t.Fatalf("stale authorization error = %v", err)
			}
			batches, total, listErr := fixture.store.ListBatches(t.Context(), filetransfer.BatchListFilter{Limit: 10})
			if listErr != nil || total != 0 || len(batches) != 0 {
				t.Fatalf("stale authorization persisted batches = %#v total=%d err=%v", batches, total, listErr)
			}
		})
	}
}

func TestShutdownRuntimeOwnsWorkerAndTerminalStateLifecycle(t *testing.T) {
	fixture := newTransferTestFixture(t)
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/report", FileName: "report",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkRunning(t.Context(), item.ID); err != nil || !changed {
		t.Fatalf("mark transfer running: changed=%t err=%v", changed, err)
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{{RemotePath: "/archive", FileName: "archive"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%t err=%v", changed, err)
	}

	drained, err := fixture.runtime.Shutdown(time.Second, "transfer interrupted", "batch interrupted")
	if err != nil || !drained {
		t.Fatalf("shutdown: drained=%t err=%v", drained, err)
	}
	storedItem, err := fixture.store.Get(t.Context(), item.ID)
	if err != nil || storedItem.Status != filetransfer.StatusFailed || storedItem.Error != "transfer interrupted" {
		t.Fatalf("transfer after shutdown = %#v, %v", storedItem, err)
	}
	storedBatch, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || storedBatch.Status != filetransfer.StatusFailed || storedBatch.Error != "batch interrupted" {
		t.Fatalf("batch after shutdown = %#v, %v", storedBatch, err)
	}
}

func TestUploadRunnerKeepsStagingWhenClaimFails(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runtime := fixture.runtime
	store := fixture.store
	handlers := fixture.handlers
	runtimeID := fixture.runtimeID
	staged, _, _, err := handlers.runner.StageUploadFile(strings.NewReader("owned staging"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: staged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_runner_history_projection BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	handlers.runner.RunUpload(context.Background(), runtime, item.ID, false, fixture.execution(t).runnerExecution())
	assertRunnerTransferAndStaging(t, store, item.ID, staged, filetransfer.StatusPending)
}

func TestUploadRunnerWithoutClaimDoesNotDeleteOwnedStaging(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runtime := fixture.runtime
	store := fixture.store
	handlers := fixture.handlers
	runtimeID := fixture.runtimeID
	staged, _, _, err := handlers.runner.StageUploadFile(strings.NewReader("owned staging"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: staged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.MarkRunning(context.Background(), item.ID); err != nil || !changed {
		t.Fatalf("claim upload: changed=%v err=%v", changed, err)
	}

	handlers.runner.RunUpload(context.Background(), runtime, item.ID, false, fixture.execution(t).runnerExecution())
	assertRunnerTransferAndStaging(t, store, item.ID, staged, filetransfer.StatusRunning)
}

func TestDownloadRunnerKeepsReservationWhenClaimFails(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runtime := fixture.runtime
	store := fixture.store
	handlers := fixture.handlers
	runtimeID := fixture.runtimeID
	reserved, err := handlers.runner.ReserveDownloadTempFile()
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		RemotePath: "/fixture", FileName: "fixture", TempPath: reserved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_download_runner_history_projection BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	handlers.runner.RunDownload(context.Background(), runtime, item.ID, fixture.execution(t).runnerExecution())
	assertRunnerTransferAndStaging(t, store, item.ID, reserved, filetransfer.StatusPending)
}

func TestTransferRunnerRetainsTempUntilFailureIsDurable(t *testing.T) {
	for _, direction := range []string{filetransfer.DirectionUpload, filetransfer.DirectionDownload} {
		t.Run(direction, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			var tempPath string
			var err error
			if direction == filetransfer.DirectionUpload {
				tempPath, _, _, err = fixture.handlers.runner.StageUploadFile(strings.NewReader("recoverable transfer data"))
			} else {
				tempPath, err = fixture.handlers.runner.ReserveDownloadTempFile()
			}
			if err != nil {
				t.Fatal(err)
			}
			item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
				RuntimeID: fixture.runtimeID, Direction: direction, Source: filetransfer.SourceUI,
				RemotePath: "/fixture", FileName: "fixture", TempPath: tempPath,
			})
			if err != nil {
				t.Fatal(err)
			}
			trigger := "reject_" + direction + "_terminal_projection"
			if _, err := fixture.database.Exec(`CREATE TRIGGER ` + trigger + ` BEFORE UPDATE OF status ON history_entries
				WHEN NEW.status IN ('failed', 'canceled')
				BEGIN SELECT RAISE(ABORT, 'injected terminal projection failure'); END`); err != nil {
				t.Fatal(err)
			}
			execution := fixture.execution(t)

			done := make(chan struct{})
			go func() {
				defer close(done)
				if direction == filetransfer.DirectionUpload {
					fixture.handlers.runner.RunUpload(context.Background(), fixture.runtime, item.ID, false, execution.runnerExecution())
				} else {
					fixture.handlers.runner.RunDownload(context.Background(), fixture.runtime, item.ID, execution.runnerExecution())
				}
			}()
			waitForTransferStatus(t, fixture.store, item.ID, filetransfer.StatusRunning)
			time.Sleep(300 * time.Millisecond)
			select {
			case <-done:
				t.Fatal("runner exited before terminal state became durable")
			default:
			}
			if _, err := os.Stat(tempPath); err != nil {
				t.Fatalf("recoverable temp was removed before terminal persistence: %v", err)
			}
			if _, err := fixture.database.Exec(`DROP TRIGGER ` + trigger); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("runner did not recover after terminal persistence became writable")
			}
			stored, err := fixture.store.Get(t.Context(), item.ID)
			if err != nil || stored.Status != filetransfer.StatusFailed {
				t.Fatalf("terminal transfer = %#v, %v", stored, err)
			}
			if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("durably failed transfer temp remains at %q: %v", tempPath, err)
			}
		})
	}
}

func waitForTransferStatus(t *testing.T, store *filetransfer.Store, id int64, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		item, err := store.Get(t.Context(), id)
		if err == nil && item.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	item, err := store.Get(t.Context(), id)
	t.Fatalf("transfer status = %q, want %q (error=%v)", item.Status, want, err)
}

func TestBatchArchivePersistenceFailureCannotFinalizeAsCompleted(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runtime := fixture.runtime
	store := fixture.store
	handlers := fixture.handlers
	runtimeID := fixture.runtimeID
	batch, err := store.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{
			{RemotePath: "/one", FileName: "one", TempPath: filepath.Join(t.TempDir(), "one")},
			{RemotePath: "/two", FileName: "two", TempPath: filepath.Join(t.TempDir(), "two")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
	}
	archivePath := filepath.Join(t.TempDir(), "download.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_runner_archive BEFORE UPDATE OF archive_path ON file_transfer_batches
		BEGIN SELECT RAISE(ABORT, 'injected archive persistence failure'); END`); err != nil {
		t.Fatal(err)
	}

	if handlers.runner.PersistDownloadBatchArchive(t.Context(), runtime, batch, archivePath, nil) {
		t.Fatal("archive persistence failure reported success")
	}
	stored, err := store.GetBatch(context.Background(), batch.ID)
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
	fixture := newTransferTestFixture(t)
	runtime := fixture.runtime
	store := fixture.store
	handlers := fixture.handlers
	runtimeID := fixture.runtimeID
	batch, err := store.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{{RemotePath: "/one", FileName: "one", TempPath: filepath.Join(t.TempDir(), "one")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
	}
	archivePath := filepath.Join(t.TempDir(), "download.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_runner_history_projection BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() { result <- handlers.runner.PersistDownloadBatchArchive(ctx, runtime, batch, archivePath, nil) }()
	time.Sleep(300 * time.Millisecond)
	select {
	case <-result:
		t.Fatal("archive persistence runner exited before terminal state was durable")
	default:
	}
	stored, err := store.GetBatch(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusRunning || stored.ArchivePath != "" {
		t.Fatalf("unpersisted failure changed batch = %#v", stored)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("recoverable archive was removed: %v", err)
	}
	if _, err := fixture.database.Exec(`DROP TRIGGER reject_runner_history_projection`); err != nil {
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
	stored, err = store.GetBatch(context.Background(), batch.ID)
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

func assertRunnerTransferAndStaging(t *testing.T, store *filetransfer.Store, id int64, staged, wantStatus string) {
	t.Helper()
	item, err := store.Get(context.Background(), id)
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
