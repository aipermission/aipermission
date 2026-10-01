package transferruntime

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestDownloadBatchBudgetSurvivesPauseAndQueueReordering(t *testing.T) {
	runner, runtime, batch, _ := newNativeDownloadBudgetFixture(t, 3)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	paused := make(chan struct{})
	done := make(chan struct{})
	adapter := &byteBudgetAdapter{}
	var paths []string
	adapter.download = func(ctx context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		paths = append(paths, path)
		bytes := int64(8)
		if len(paths) == 1 {
			bytes = 6
		}
		if bytes > options.MaxBytes {
			return connectors.TransferResult{}, connectors.ErrTransferByteLimit
		}
		if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
			return connectors.TransferResult{}, err
		}
		options.Progress(bytes, 8)
		if len(paths) == 1 {
			control := runtime.BatchControl(batch.ID)
			if control == nil || !control.Pause() {
				return connectors.TransferResult{Bytes: bytes}, errors.New("pause control missing")
			}
			if changed, err := runtime.store.PauseBatch(ctx, batch.ID); err != nil || !changed {
				return connectors.TransferResult{Bytes: bytes}, errors.New("pause was not durable")
			}
			close(paused)
			if err := options.Wait(ctx); err != nil {
				return connectors.TransferResult{Bytes: bytes}, err
			}
			return connectors.TransferResult{Bytes: bytes}, errors.New("partial attempt failed after resume")
		}
		return connectors.TransferResult{Bytes: bytes}, nil
	}
	go func() {
		defer close(done)
		runner.runBatch(ctx, runtime, batch.ID, false, Execution{Adapter: adapter, Boundary: actionresult.NewCredentialBoundary(nil)})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("download runner did not stop")
		}
	})
	select {
	case <-paused:
	case <-done:
		t.Fatal("runner exited before pause")
	case <-ctx.Done():
		t.Fatal("runner did not reach pause")
	}
	if removed, err := runtime.store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[2].ID, batch.Items[1].ID}); err != nil || len(removed) != 0 {
		t.Fatalf("reorder removed=%v err=%v", removed, err)
	}
	if changed, err := runtime.store.ResumeBatch(ctx, batch.ID); err != nil || !changed {
		t.Fatalf("resume changed=%v err=%v", changed, err)
	}
	if !runtime.BatchControl(batch.ID).Resume() {
		t.Fatal("in-memory pause gate did not resume")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("resumed batch did not finish")
	}
	wantPaths := []string{batch.Items[0].TempPath, batch.Items[2].TempPath, batch.Items[1].TempPath}
	if !reflect.DeepEqual(paths, wantPaths) || !reflect.DeepEqual(adapter.limits, []int64{8, 8, 2}) {
		t.Fatalf("pause reset budget/order: paths=%v limits=%v", paths, adapter.limits)
	}
	stored, err := runtime.store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != filetransfer.StatusFailed || stored.FailureKind != filetransfer.FailureKindValidation || stored.TransferredBytes != 14 || stored.ArchivePath != "" {
		t.Fatalf("resumed batch published or lost bytes: batch=%#v err=%v", stored, err)
	}
	assertNativeDownloadTempsRemoved(t, batch)
}
