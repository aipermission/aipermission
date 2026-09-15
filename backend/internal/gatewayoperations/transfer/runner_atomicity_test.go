package gatewaytransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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

type cancelCommitTransferAdapter struct {
	rejectingTransferAdapter
	started chan struct{}
}

type uncertainUploadTransferAdapter struct{ rejectingTransferAdapter }

type recoverableStagingTransferAdapter struct {
	rejectingTransferAdapter
	recovered []string
}

type blockingStagingRecoveryAdapter struct {
	rejectingTransferAdapter
	calls     atomic.Int32
	recovered atomic.Int32
}

type retryingStagingRecoveryAdapter struct {
	rejectingTransferAdapter
	calls     atomic.Int32
	recovered chan struct{}
}

func (adapter *blockingStagingRecoveryAdapter) CleanupRemoteStaging(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, ref string) error {
	adapter.calls.Add(1)
	if ref == "opaque-0" {
		<-ctx.Done()
		return ctx.Err()
	}
	adapter.recovered.Add(1)
	return nil
}

func (adapter *retryingStagingRecoveryAdapter) CleanupRemoteStaging(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string) error {
	if adapter.calls.Add(1) == 1 {
		return errors.New("temporary cleanup failure")
	}
	close(adapter.recovered)
	return nil
}

func (adapter *recoverableStagingTransferAdapter) UploadFile(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _ string, _ string, _ bool, options connectors.TransferOptions) (connectors.TransferResult, error) {
	if err := options.RecordStaging(ctx, "/tmp/.aipermission-upload-0123456789abcdef0123456789abcdef-0.tmp"); err != nil {
		return connectors.TransferResult{}, err
	}
	return connectors.TransferResult{}, errors.New("simulated process interruption")
}

func (adapter *recoverableStagingTransferAdapter) CleanupRemoteStaging(_ context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, ref string) error {
	adapter.recovered = append(adapter.recovered, ref)
	return nil
}

const uncertainUploadChecksum = "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"

func (uncertainUploadTransferAdapter) UploadFile(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string, string, bool, connectors.TransferOptions) (connectors.TransferResult, error) {
	return connectors.TransferResult{Bytes: 7, Size: 7, ChecksumSHA256: uncertainUploadChecksum}, connectors.ClassifyOutcomeUnknown(
		"atomic_replace", map[string]any{
			"recovery_hint":       "inspect destination",
			"remote_staging_path": "/tmp/.aipermission-upload-stage.tmp",
		}, errors.New("rename reply lost"),
	)
}

func TestUncertainUploadRetainsStagingAndTransferEvidence(t *testing.T) {
	fixture := newTransferTestFixtureWithAdapter(t, uncertainUploadTransferAdapter{})
	fixture.handlers.runner = transferapp.NewRunner(transferapp.RunnerConfig{DataPath: fixture.dataPath, TempTTL: time.Hour})
	tempPath, size, checksum, err := fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/remote", FileName: "upload", TempPath: tempPath, SizeBytes: size, ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handlers.runner.RunUpload(t.Context(), fixture.runtime, item.ID, true, fixture.execution(t).runnerExecution())
	stored, err := fixture.store.Get(t.Context(), item.ID)
	if err != nil || stored.FailureKind != filetransfer.FailureKindOutcomeUnknown || stored.TransferredBytes != 7 || stored.ChecksumSHA256 != uncertainUploadChecksum || stored.TempExpiresAt == "" {
		t.Fatalf("uncertain transfer=%#v err=%v", stored, err)
	}
	if stored.FailureDetails["remote_staging_path"] != "/tmp/.aipermission-upload-stage.tmp" || stored.FailureDetails["recovery_hint"] != "inspect destination" {
		t.Fatalf("uncertain transfer recovery details=%#v", stored.FailureDetails)
	}
	var preview string
	if err := fixture.database.QueryRowContext(t.Context(), `
		SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, item.ID).Scan(&preview); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, `"remote_staging_path":"/tmp/.aipermission-upload-stage.tmp"`) {
		t.Fatalf("history preview lost recovery details: %s", preview)
	}
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("uncertain upload staging was not retained: %v", err)
	}
}

func TestTransferTempRecoverySurvivesRuntimeRestart(t *testing.T) {
	fixture := newTransferTestFixtureWithAdapter(t, uncertainUploadTransferAdapter{})
	fixture.handlers.runner = transferapp.NewRunner(transferapp.RunnerConfig{DataPath: fixture.dataPath, TempTTL: time.Hour})
	tempPath, size, checksum, err := fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/remote", FileName: "upload", TempPath: tempPath, SizeBytes: size, ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handlers.runner.RunUpload(t.Context(), fixture.runtime, item.ID, true, fixture.execution(t).runnerExecution())
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE file_transfers SET temp_expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), item.ID); err != nil {
		t.Fatal(err)
	}
	restarted := transferapp.NewRunner(transferapp.RunnerConfig{DataPath: fixture.dataPath, TempTTL: time.Hour})
	if err := restarted.RecoverTempCleanup(t.Context(), fixture.runtime); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, getErr := fixture.store.Get(t.Context(), item.ID)
		_, statErr := os.Stat(tempPath)
		if getErr == nil && stored.TempPath == "" && os.IsNotExist(statErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart cleanup did not remove staging: record=%#v get_err=%v stat_err=%v", stored, getErr, statErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoteUploadStagingRecoverySurvivesRuntimeRestart(t *testing.T) {
	adapter := &recoverableStagingTransferAdapter{}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	tempPath, size, checksum, err := fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/remote", FileName: "upload", TempPath: tempPath, SizeBytes: size, ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handlers.runner.RunUpload(t.Context(), fixture.runtime, item.ID, true, fixture.execution(t).runnerExecution())
	stored, err := fixture.store.Get(t.Context(), item.ID)
	if err != nil || stored.RemoteStagingRef == "" {
		t.Fatalf("persisted remote staging = %#v err=%v", stored, err)
	}
	restarted := transferapp.NewRunner(transferapp.RunnerConfig{
		DataPath: fixture.dataPath, TempTTL: time.Hour,
		AdapterFor: func(string) connectorapi.FileTransferAdapter { return adapter },
	})
	if err := restarted.RecoverRemoteStaging(t.Context(), fixture.runtime); err != nil {
		t.Fatal(err)
	}
	stored, err = fixture.store.Get(t.Context(), item.ID)
	if err != nil || stored.RemoteStagingRef != "" || len(adapter.recovered) != 1 {
		t.Fatalf("recovered remote staging = %#v refs=%#v err=%v", stored, adapter.recovered, err)
	}
}

func TestRemoteStagingRecoveryBoundsEachCandidateIndependently(t *testing.T) {
	adapter := &blockingStagingRecoveryAdapter{}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	for index := 0; index < 2; index++ {
		item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
			RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
			RemotePath: fmt.Sprintf("/remote-%d", index), FileName: "upload",
		})
		if err != nil {
			t.Fatal(err)
		}
		if changed, err := fixture.store.MarkRunning(t.Context(), item.ID); err != nil || !changed {
			t.Fatalf("mark running: changed=%v err=%v", changed, err)
		}
		if err := fixture.store.SetRemoteStagingRef(t.Context(), item.ID, fmt.Sprintf("opaque-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	runner := transferapp.NewRunner(transferapp.RunnerConfig{
		DataPath: fixture.dataPath, TempTTL: time.Hour, RemoteRecoveryTimeout: 20 * time.Millisecond,
		AdapterFor: func(string) connectorapi.FileTransferAdapter { return adapter },
	})
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := runner.RecoverRemoteStaging(ctx, fixture.runtime); err != nil {
		t.Fatalf("recovery error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Fatalf("per-candidate recovery deadline took %s", elapsed)
	}
	items, err := fixture.store.ListRemoteStagingCandidates(t.Context())
	if err != nil || len(items) != 1 || items[0].RemoteStagingRef != "opaque-0" {
		t.Fatalf("candidate recovery result: items=%#v err=%v", items, err)
	}
	if adapter.calls.Load() != 2 || adapter.recovered.Load() != 1 {
		t.Fatalf("candidate calls=%d recovered=%d", adapter.calls.Load(), adapter.recovered.Load())
	}
}

func TestRemoteStagingRecoveryRetriesWhileWorkspaceRemainsOpen(t *testing.T) {
	adapter := &retryingStagingRecoveryAdapter{recovered: make(chan struct{})}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/remote", FileName: "upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkRunning(t.Context(), item.ID); err != nil || !changed {
		t.Fatalf("mark running: changed=%v err=%v", changed, err)
	}
	if err := fixture.store.SetRemoteStagingRef(t.Context(), item.ID, "opaque-retry"); err != nil {
		t.Fatal(err)
	}
	runner := transferapp.NewRunner(transferapp.RunnerConfig{
		DataPath: fixture.dataPath, TempTTL: time.Hour, RemoteRecoveryTimeout: 20 * time.Millisecond,
		RemoteRecoveryRetry: 5 * time.Millisecond,
		AdapterFor:          func(string) connectorapi.FileTransferAdapter { return adapter },
	})
	if !runner.StartRemoteStagingRecovery(fixture.runtime) {
		t.Fatal("remote staging recovery did not start")
	}
	select {
	case <-adapter.recovered:
	case <-time.After(time.Second):
		t.Fatal("remote staging cleanup was not retried")
	}
	deadline := time.Now().Add(time.Second)
	for {
		stored, err := fixture.store.Get(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.RemoteStagingRef == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovered staging reference was not cleared")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBatchArchiveRecoverySurvivesRuntimeRestart(t *testing.T) {
	fixture := newTransferTestFixture(t)
	root, err := fixture.handlers.runner.EnsureTempRoot(fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "download.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Status: filetransfer.StatusPending, Items: []filetransfer.CreateRequest{{RemotePath: "/remote", FileName: "remote"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SetBatchArchive(t.Context(), batch.ID, archivePath, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.handlers.runner.RecoverTempCleanup(t.Context(), fixture.runtime); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, getErr := fixture.store.GetBatch(t.Context(), batch.ID)
		_, statErr := os.Stat(archivePath)
		if getErr == nil && stored.ArchivePath == "" && os.IsNotExist(statErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart cleanup did not remove archive: batch=%#v get_err=%v stat_err=%v", stored, getErr, statErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBatchCleanupPreservesUncertainUploadStaging(t *testing.T) {
	fixture := newTransferTestFixture(t)
	first, _, _, err := fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("uncertain"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("failed"))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{
			{RemotePath: "/uncertain", FileName: "uncertain", TempPath: first},
			{RemotePath: "/failed", FileName: "failed", TempPath: second},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := fixture.store.FailWithKind(t.Context(), batch.Items[0].ID, "unknown", filetransfer.FailureKindOutcomeUnknown); err != nil || !ok {
		t.Fatalf("mark uncertain item: ok=%v err=%v", ok, err)
	}
	if ok, err := fixture.store.FailWithKind(t.Context(), batch.Items[1].ID, "failed", filetransfer.FailureKindTimeout); err != nil || !ok {
		t.Fatalf("mark failed item: ok=%v err=%v", ok, err)
	}

	fixture.handlers.runner.CleanupBatchTemps(fixture.runtime, batch.ID)
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("uncertain batch staging was removed: %v", err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("definitive batch staging was retained: %v", err)
	}
}

func (adapter *cancelCommitTransferAdapter) UploadFile(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _ string, _ string, _ bool, _ connectors.TransferOptions) (connectors.TransferResult, error) {
	close(adapter.started)
	<-ctx.Done()
	return connectors.TransferResult{Bytes: 7, ChecksumSHA256: "remote-commit"}, nil
}

func TestCancelWaitsForRemoteUploadOutcomeBeforePersistingStatus(t *testing.T) {
	adapter := &cancelCommitTransferAdapter{started: make(chan struct{})}
	fixture := newTransferTestFixtureWithAdapter(t, adapter)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) { return fixture.runtime, true }
	tempPath := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(tempPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, Source: filetransfer.SourceUI,
		RemotePath: "/remote", FileName: "upload", TempPath: tempPath, SizeBytes: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.handlers.runner.LaunchUpload(t.Context(), fixture.runtime, item.ID, false, fixture.execution(t).runnerExecution()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-adapter.started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	request := httptest.NewRequest(http.MethodPost, "/transfers/cancel", nil)
	request.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	response := httptest.NewRecorder()
	fixture.handlers.CancelFileTransfer(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel response=%d %s", response.Code, response.Body.String())
	}
	stored, err := fixture.store.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != filetransfer.StatusCompleted || stored.TransferredBytes != 7 {
		t.Fatalf("remote commit hidden by cancel: %#v", stored)
	}
}

func (adapter *revisionGateTransferAdapter) DownloadFile(ctx context.Context, _ connectorapi.FileTransferGateway, runtime connectorapi.TransferRuntime, runtimeID int64, _ string, _ string, _ connectors.TransferOptions) (connectors.TransferResult, error) {
	close(adapter.started)
	select {
	case <-ctx.Done():
		return connectors.TransferResult{}, ctx.Err()
	case <-adapter.proceed:
	}
	if _, _, _, err := runtime.TargetProfileByRuntimeID(ctx, runtimeID); err != nil {
		return connectors.TransferResult{}, err
	}
	adapter.remoteCalls.Add(1)
	return connectors.TransferResult{}, errors.New("remote operation should not start after profile drift")
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
	staged, _, _, err := handlers.runner.StageUploadFile(runtime, strings.NewReader("owned staging"))
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
	staged, _, _, err := handlers.runner.StageUploadFile(runtime, strings.NewReader("owned staging"))
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
	reserved, err := handlers.runner.ReserveDownloadTempFile(runtime)
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

func TestTempRecoveryScavengesOnlyOldUnreferencedOwnedFiles(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runner := transferapp.NewRunner(transferapp.RunnerConfig{DataPath: fixture.dataPath, TempTTL: time.Hour})
	root, err := runner.EnsureTempRoot(fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{
		"old":        filepath.Join(root, "upload-old"),
		"fresh":      filepath.Join(root, "download-fresh"),
		"referenced": filepath.Join(root, "archive-referenced.zip"),
		"unknown":    filepath.Join(root, "notes.txt"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, key := range []string{"old", "referenced", "unknown"} {
		if err := os.Chtimes(paths[key], old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		RemotePath: "/referenced", FileName: "referenced", TempPath: paths["referenced"],
	}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "upload-link")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	if err := runner.RecoverTempCleanup(t.Context(), fixture.runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths["old"]); !os.IsNotExist(err) {
		t.Fatalf("old orphan was not removed: %v", err)
	}
	for _, key := range []string{"fresh", "referenced", "unknown"} {
		if _, err := os.Stat(paths[key]); err != nil {
			t.Fatalf("%s temp was removed: %v", key, err)
		}
	}
	if _, err := os.Lstat(symlink); err != nil {
		t.Fatalf("symlink was removed: %v", err)
	}
	if contents, err := os.ReadFile(outside); err != nil || string(contents) != "preserved" {
		t.Fatalf("symlink target changed: contents=%q err=%v", contents, err)
	}
}

func TestPeriodicTempRecoveryRetriesFailedDeletion(t *testing.T) {
	fixture := newTransferTestFixture(t)
	runner := transferapp.NewRunner(transferapp.RunnerConfig{
		DataPath: fixture.dataPath, TempTTL: time.Hour, TempCleanupRetry: 10 * time.Millisecond,
	})
	root, err := runner.EnsureTempRoot(fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(root, "download-retry")
	if err := os.Mkdir(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(staged, "still-open")
	if err := os.WriteFile(blocker, []byte("block first cleanup"), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		RemotePath: "/retry", FileName: "retry", TempPath: staged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkRunning(t.Context(), item.ID); err != nil || !changed {
		t.Fatalf("mark running: changed=%v err=%v", changed, err)
	}
	if changed, err := fixture.store.FailWithKind(t.Context(), item.ID, "failed", filetransfer.FailureKindUnknown); err != nil || !changed {
		t.Fatalf("fail transfer: changed=%v err=%v", changed, err)
	}
	if err := fixture.store.SetTempExpiry(t.Context(), item.ID, staged, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !runner.StartTempCleanupRecovery(fixture.runtime) {
		t.Fatal("periodic temp cleanup did not start")
	}
	time.Sleep(30 * time.Millisecond)
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, getErr := fixture.store.Get(t.Context(), item.ID)
		_, statErr := os.Stat(staged)
		if getErr == nil && stored.TempPath == "" && os.IsNotExist(statErr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, _ := fixture.store.Get(t.Context(), item.ID)
	_, statErr := os.Stat(staged)
	t.Fatalf("periodic cleanup did not reconcile staging: record=%#v stat=%v", stored, statErr)
}

func TestTransferRunnerRetainsTempUntilFailureIsDurable(t *testing.T) {
	for _, direction := range []string{filetransfer.DirectionUpload, filetransfer.DirectionDownload} {
		t.Run(direction, func(t *testing.T) {
			fixture := newTransferTestFixture(t)
			var tempPath string
			var err error
			if direction == filetransfer.DirectionUpload {
				tempPath, _, _, err = fixture.handlers.runner.StageUploadFile(fixture.runtime, strings.NewReader("recoverable transfer data"))
			} else {
				tempPath, err = fixture.handlers.runner.ReserveDownloadTempFile(fixture.runtime)
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

	if handlers.runner.PersistDownloadBatchArchive(t.Context(), runtime, &batch, archivePath, nil) {
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

func TestBatchArchivePersistenceKeepsRunnerAndStoreExpiryAligned(t *testing.T) {
	fixture := newTransferTestFixture(t)
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{{RemotePath: "/one", FileName: "one"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "download.zip")
	if !fixture.handlers.runner.PersistDownloadBatchArchive(t.Context(), fixture.runtime, &batch, archivePath, nil) {
		t.Fatal("archive persistence reported failure")
	}
	stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || batch.ArchiveExpiresAt == "" || batch.ArchiveExpiresAt != stored.ArchiveExpiresAt {
		t.Fatalf("runner archive=%#v stored=%#v err=%v", batch, stored, err)
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
	go func() { result <- handlers.runner.PersistDownloadBatchArchive(ctx, runtime, &batch, archivePath, nil) }()
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
