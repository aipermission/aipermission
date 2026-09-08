package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/transferjobs"
)

func TestFileTransferControlRoutesDriveRegisteredBatch(t *testing.T) {
	fixture := newAPITestFixture(t)
	item := createS3IdentityRuntime(t, fixture.server, "http://127.0.0.1:9")
	runtime := fixture.server.activeRuntime()
	batch, err := runtime.fileTransfers.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: item.TransferRuntimeID, Direction: filetransfer.DirectionDownload,
		Items: []filetransfer.CreateRequest{{RemotePath: "/report", FileName: "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("start batch: %t %v", changed, err)
	}
	control := &transferjobs.Control{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime.transferJobs.Batches.RegisterControl(batch.ID, control)
	runtime.transferJobs.Batches.RegisterCancel(batch.ID, cancel)
	request := func(action string, wantCode int, wantStatus string) {
		t.Helper()
		response := performJSON(fixture.server.Handler(), http.MethodPost, fmt.Sprintf("/api/file-transfer-batches/%d/%s", batch.ID, action), "", map[string]any{})
		if response.Code != wantCode {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
		stored, err := runtime.fileTransfers.GetBatch(t.Context(), batch.ID)
		if err != nil || stored.Status != wantStatus {
			t.Fatalf("%s persisted status = %q: %v", action, stored.Status, err)
		}
	}
	request("pause", http.StatusOK, filetransfer.StatusPaused)
	request("pause", http.StatusConflict, filetransfer.StatusPaused)
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
			t.Fatal("route did not release paused transfer")
		}
	}
	resumed := wait()
	request("resume", http.StatusOK, filetransfer.StatusRunning)
	checkWait(resumed, nil)
	request("resume", http.StatusConflict, filetransfer.StatusRunning)
	request("pause", http.StatusOK, filetransfer.StatusPaused)
	canceled := wait()
	request("cancel", http.StatusOK, filetransfer.StatusCanceled)
	checkWait(canceled, context.Canceled)
	request("resume", http.StatusConflict, filetransfer.StatusCanceled)
	request("pause", http.StatusConflict, filetransfer.StatusCanceled)
	// A rejected pause must restore the gate, even after the persisted job ended.
	probe, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := control.Wait(probe); err != nil {
		t.Fatalf("rejected pause left gate closed: %v", err)
	}
}

func TestFileTransferCancelSignalsWorkerOnlyAfterTerminalStateIsDurable(t *testing.T) {
	fixture := newAPITestFixture(t)
	identity := createS3IdentityRuntime(t, fixture.server, "http://127.0.0.1:9")
	runtime := fixture.server.activeRuntime()
	item, err := runtime.fileTransfers.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: identity.TransferRuntimeID, Direction: filetransfer.DirectionUpload, RemotePath: "/report", FileName: "report",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkRunning(t.Context(), item.ID); err != nil || !changed {
		t.Fatalf("start transfer: changed=%t err=%v", changed, err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime.transferJobs.Files.RegisterCancel(item.ID, cancel)
	defer runtime.transferJobs.Files.UnregisterCancel(item.ID)

	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_transfer_cancel_history BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodPost, fmt.Sprintf("/api/file-transfers/%d/cancel", item.ID), "", map[string]any{})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed persistence response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() != nil {
		t.Fatal("worker was canceled before terminal state became durable")
	}
	stored, err := runtime.fileTransfers.Get(t.Context(), item.ID)
	if err != nil || stored.Status != filetransfer.StatusRunning {
		t.Fatalf("rolled-back transfer status=%q err=%v", stored.Status, err)
	}
	if _, err := fixture.db.Exec(`DROP TRIGGER reject_transfer_cancel_history`); err != nil {
		t.Fatal(err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodPost, fmt.Sprintf("/api/file-transfers/%d/cancel", item.ID), "", map[string]any{})
	if response.Code != http.StatusOK {
		t.Fatalf("durable cancel response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() == nil {
		t.Fatal("durably canceled transfer did not signal its worker")
	}
}

func TestFileTransferBatchCancelSignalsWorkerOnlyAfterTerminalStateIsDurable(t *testing.T) {
	fixture := newAPITestFixture(t)
	identity := createS3IdentityRuntime(t, fixture.server, "http://127.0.0.1:9")
	runtime := fixture.server.activeRuntime()
	batch, err := runtime.fileTransfers.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: identity.TransferRuntimeID, Direction: filetransfer.DirectionDownload,
		Items: []filetransfer.CreateRequest{{RemotePath: "/report", FileName: "report"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := runtime.fileTransfers.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("start batch: changed=%t err=%v", changed, err)
	}
	workerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime.transferJobs.Batches.RegisterCancel(batch.ID, cancel)
	defer runtime.transferJobs.Batches.UnregisterCancel(batch.ID)

	if _, err := fixture.db.Exec(`CREATE TRIGGER reject_batch_cancel_history BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodPost, fmt.Sprintf("/api/file-transfer-batches/%d/cancel", batch.ID), "", map[string]any{})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed persistence response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() != nil {
		t.Fatal("batch worker was canceled before terminal state became durable")
	}
	stored, err := runtime.fileTransfers.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusRunning {
		t.Fatalf("rolled-back batch status=%q err=%v", stored.Status, err)
	}
	if _, err := fixture.db.Exec(`DROP TRIGGER reject_batch_cancel_history`); err != nil {
		t.Fatal(err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodPost, fmt.Sprintf("/api/file-transfer-batches/%d/cancel", batch.ID), "", map[string]any{})
	if response.Code != http.StatusOK {
		t.Fatalf("durable cancel response=%d body=%s", response.Code, response.Body.String())
	}
	if workerCtx.Err() == nil {
		t.Fatal("durably canceled batch did not signal its worker")
	}
}
