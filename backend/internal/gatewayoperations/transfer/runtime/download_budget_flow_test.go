package transferruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type byteBudgetAdapter struct {
	connectorapi.FileTransferAdapter
	limits   []int64
	download func(context.Context, string, connectors.TransferOptions) (connectors.TransferResult, error)
}

func (adapter *byteBudgetAdapter) DownloadFile(ctx context.Context, _ connectorapi.FileTransferGateway, _ connectorapi.TransferRuntime, _ int64, _ string, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
	adapter.limits = append(adapter.limits, options.MaxBytes)
	return adapter.download(ctx, path, options)
}

func (*byteBudgetAdapter) StatRemotePath(context.Context, connectorapi.FileTransferGateway, connectorapi.TransferRuntime, int64, string) (connectors.RemotePathStatus, error) {
	return connectors.RemotePathStatus{Exists: true, Type: "file", Size: 1}, nil
}

func TestDownloadBudgetFlowCountsRemovedFailedAttemptBeforeNextDispatch(t *testing.T) {
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data.aipdb"), MaxObjectBytes: 8, MaxBatchBytes: 16})
	runtime := &Runtime{storageID: "fixture"}
	budget := &batchDownloadBudget{limit: 16}
	readErr := errors.New("fixture reader failed")
	adapter := &byteBudgetAdapter{}
	adapter.download = func(_ context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		count := options.MaxBytes
		if len(adapter.limits) == 1 {
			count = 6
		}
		if err := os.WriteFile(path, make([]byte, count), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Progress(min(count, 4), count)
		if len(adapter.limits) == 1 {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			return connectors.TransferResult{Bytes: count}, readErr
		}
		return connectors.TransferResult{Bytes: count}, nil
	}
	for index := 0; index < 4; index++ {
		path, err := runner.ReserveDownloadTempFile(runtime)
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.downloadBatchItem(t.Context(), runtime, filetransfer.Record{TempPath: path}, Execution{Adapter: adapter}, connectors.TransferOptions{}, budget)
		switch index {
		case 0:
			if !errors.Is(err, readErr) || result.Bytes != 6 {
				t.Fatalf("partial failure lost: result=%#v err=%v", result, err)
			}
		case 1, 2:
			if err != nil || result.Bytes != adapter.limits[index] {
				t.Fatalf("valid remaining bytes rejected: result=%#v err=%v", result, err)
			}
		case 3:
			if !errors.Is(err, connectors.ErrTransferByteLimit) || len(adapter.limits) != 3 {
				t.Fatalf("exhausted budget dispatched: limits=%v err=%v", adapter.limits, err)
			}
		}
	}
	if !reflect.DeepEqual(adapter.limits, []int64{8, 8, 2}) || budget.used != 16 {
		t.Fatalf("failed attempt refunded or overcharged: limits=%v used=%d", adapter.limits, budget.used)
	}
}

func TestDownloadBudgetFlowCancelsViolatingAdapterAndRejectsItsSuccess(t *testing.T) {
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data.aipdb"), MaxObjectBytes: 8})
	runtime := &Runtime{storageID: "fixture"}
	path, err := runner.ReserveDownloadTempFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &byteBudgetAdapter{download: func(ctx context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		if err := os.WriteFile(path, make([]byte, 9), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Progress(9, 9)
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("over-budget adapter was not canceled")
		}
		return connectors.TransferResult{Bytes: 9}, nil
	}}
	result, err := runner.downloadBatchItem(t.Context(), runtime, filetransfer.Record{TempPath: path}, Execution{Adapter: adapter}, connectors.TransferOptions{}, &batchDownloadBudget{limit: 16})
	if !errors.Is(err, connectors.ErrTransferByteLimit) || result.Bytes != 9 {
		t.Fatalf("violating adapter published success or lost evidence: result=%#v err=%v", result, err)
	}
}

func TestDownloadBudgetFlowCancellationPreservesActualConsumption(t *testing.T) {
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data.aipdb"), MaxObjectBytes: 8})
	runtime := &Runtime{storageID: "fixture"}
	path, err := runner.ReserveDownloadTempFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	adapter := &byteBudgetAdapter{download: func(ctx context.Context, path string, options connectors.TransferOptions) (connectors.TransferResult, error) {
		if err := os.WriteFile(path, make([]byte, 6), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Progress(6, 8)
		cancel()
		return connectors.TransferResult{}, ctx.Err()
	}}
	budget := &batchDownloadBudget{limit: 16}
	result, err := runner.downloadBatchItem(ctx, runtime, filetransfer.Record{TempPath: path}, Execution{Adapter: adapter}, connectors.TransferOptions{}, budget)
	if !errors.Is(err, context.Canceled) || result.Bytes != 6 || budget.used != 6 {
		t.Fatalf("cancellation lost evidence: result=%#v used=%d err=%v", result, budget.used, err)
	}
}
