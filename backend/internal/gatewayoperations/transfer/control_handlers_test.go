package gatewaytransfer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	transferapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer/runtime"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestFileTransferControlHandlersDriveRegisteredBatch(t *testing.T) {
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload,
		Items: []filetransfer.CreateRequest{{RemotePath: "/report", FileName: "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("start batch: %t %v", changed, err)
	}
	control := &transferjobs.Control{}
	ctx, cancel := context.WithCancel(t.Context())
	fixture.jobs.Batches.RegisterControl(batch.ID, control)
	fixture.jobs.Batches.RegisterCancel(batch.ID, cancel)
	workerDone := make(chan struct{})
	defer func() {
		cancel()
		<-workerDone
	}()
	go func() {
		<-ctx.Done()
		_, _ = fixture.store.CancelBatch(context.Background(), batch.ID, "canceled by test worker")
		fixture.jobs.Batches.UnregisterCancel(batch.ID)
		close(workerDone)
	}()
	request := func(action string, handler http.HandlerFunc, wantCode int, wantStatus string) {
		t.Helper()
		response := performTransferControlRequest(handler, batch.ID)
		if response.Code != wantCode {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
		stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
		if err != nil || stored.Status != wantStatus {
			t.Fatalf("%s persisted status = %q: %v", action, stored.Status, err)
		}
	}
	request("pause", fixture.handlers.PauseFileTransferBatch, http.StatusOK, filetransfer.StatusPaused)
	request("pause", fixture.handlers.PauseFileTransferBatch, http.StatusConflict, filetransfer.StatusPaused)
	wait := func() <-chan error {
		done := make(chan error, 1)
		go func() { done <- control.Wait(ctx) }()
		return done
	}
	checkWait := func(done <-chan error, expected error) {
		t.Helper()
		select {
		case err := <-done:
			if !errors.Is(err, expected) {
				t.Fatalf("wait = %v, want %v", err, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("handler did not release paused transfer")
		}
	}
	resumed := wait()
	request("resume", fixture.handlers.ResumeFileTransferBatch, http.StatusOK, filetransfer.StatusRunning)
	checkWait(resumed, nil)
	request("resume", fixture.handlers.ResumeFileTransferBatch, http.StatusConflict, filetransfer.StatusRunning)
	request("pause", fixture.handlers.PauseFileTransferBatch, http.StatusOK, filetransfer.StatusPaused)
	canceled := wait()
	request("cancel", fixture.handlers.CancelFileTransferBatch, http.StatusOK, filetransfer.StatusCanceled)
	checkWait(canceled, context.Canceled)
	request("resume", fixture.handlers.ResumeFileTransferBatch, http.StatusConflict, filetransfer.StatusCanceled)
	request("pause", fixture.handlers.PauseFileTransferBatch, http.StatusConflict, filetransfer.StatusCanceled)
	// A rejected pause must restore the gate, even after the persisted job ended.
	probe, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := control.Wait(probe); err != nil {
		t.Fatalf("rejected pause left gate closed: %v", err)
	}
}

func TestFileTransferCancelSignalsWorkerBeforeReconcilingTerminalState(t *testing.T) {
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}
	item, err := fixture.store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionUpload, RemotePath: "/report", FileName: "report",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkRunning(t.Context(), item.ID); err != nil || !changed {
		t.Fatalf("start transfer: changed=%t err=%v", changed, err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	fixture.jobs.Files.RegisterCancel(item.ID, cancel)
	workerDone := make(chan struct{})
	defer func() {
		cancel()
		<-workerDone
	}()
	go func() {
		<-workerCtx.Done()
		_, _ = fixture.store.Cancel(context.Background(), item.ID, "canceled by test worker")
		fixture.jobs.Files.UnregisterCancel(item.ID)
		close(workerDone)
	}()

	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_transfer_cancel_history BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := performTransferControlRequest(fixture.handlers.CancelFileTransfer, item.ID)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed persistence response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() == nil {
		t.Fatal("worker was not signaled before terminal-state reconciliation")
	}
	stored, err := fixture.store.Get(t.Context(), item.ID)
	if err != nil || stored.Status != filetransfer.StatusRunning {
		t.Fatalf("rolled-back transfer status=%q err=%v", stored.Status, err)
	}
	if _, err := fixture.database.Exec(`DROP TRIGGER reject_transfer_cancel_history`); err != nil {
		t.Fatal(err)
	}
	response = performTransferControlRequest(fixture.handlers.CancelFileTransfer, item.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("reconciled cancel response=%d body=%s", response.Code, response.Body.String())
	}
	stored, err = fixture.store.Get(t.Context(), item.ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindOutcomeUnknown {
		t.Fatalf("uncertain transfer outcome=%#v err=%v", stored, err)
	}
	<-workerDone
}

func TestFileTransferBatchCancelSignalsWorkerBeforeReconcilingTerminalState(t *testing.T) {
	fixture := newTransferTestFixture(t)
	fixture.handlers.scope = func(http.ResponseWriter) (*transferapp.Runtime, bool) {
		return fixture.runtime, true
	}
	batch, err := fixture.store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: fixture.runtimeID, Direction: filetransfer.DirectionDownload,
		Items: []filetransfer.CreateRequest{{RemotePath: "/report", FileName: "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := fixture.store.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("start batch: changed=%t err=%v", changed, err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	fixture.jobs.Batches.RegisterCancel(batch.ID, cancel)
	workerDone := make(chan struct{})
	defer func() {
		cancel()
		<-workerDone
	}()
	go func() {
		<-workerCtx.Done()
		_, _ = fixture.store.CancelBatch(context.Background(), batch.ID, "canceled by test worker")
		fixture.jobs.Batches.UnregisterCancel(batch.ID)
		close(workerDone)
	}()

	if _, err := fixture.database.Exec(`CREATE TRIGGER reject_batch_cancel_history BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := performTransferControlRequest(fixture.handlers.CancelFileTransferBatch, batch.ID)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed persistence response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() == nil {
		t.Fatal("batch worker was not signaled before terminal-state reconciliation")
	}
	stored, err := fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusRunning {
		t.Fatalf("rolled-back batch status=%q err=%v", stored.Status, err)
	}
	if _, err := fixture.database.Exec(`DROP TRIGGER reject_batch_cancel_history`); err != nil {
		t.Fatal(err)
	}
	response = performTransferControlRequest(fixture.handlers.CancelFileTransferBatch, batch.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("reconciled cancel response=%d body=%s", response.Code, response.Body.String())
	}
	stored, err = fixture.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindOutcomeUnknown {
		t.Fatalf("uncertain batch outcome=%#v err=%v", stored, err)
	}
	<-workerDone
}

func performTransferControlRequest(handler http.HandlerFunc, id int64) *httptest.ResponseRecorder {
	idValue := strconv.FormatInt(id, 10)
	request := httptest.NewRequest(http.MethodPost, "/transfers/"+idValue+"/control", nil)
	request.SetPathValue("id", idValue)
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}
