package transferruntime

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestDownloadBudgetHighWaterDoesNotRefundRemovedPartialFiles(t *testing.T) {
	budget := &batchDownloadBudget{limit: 16}
	attempt, err := budget.begin(8)
	if err != nil {
		t.Fatal(err)
	}
	for _, bytes := range []int64{4, 6, 3, 6} {
		if err := attempt.record(bytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := attempt.reconcile(filepath.Join(t.TempDir(), "already-removed"), 0, false); err != nil {
		t.Fatal(err)
	}
	if budget.used != 6 || attempt.bytes() != 6 {
		t.Fatalf("partial bytes were duplicated or refunded: used=%d bytes=%d", budget.used, attempt.bytes())
	}
	for _, want := range []int64{8, 2} {
		next, err := budget.begin(8)
		if err != nil || next.limit != want {
			t.Fatalf("remaining budget: attempt=%#v err=%v want=%d", next, err, want)
		}
		if err := next.record(want); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := budget.begin(8); !errors.Is(err, connectors.ErrTransferByteLimit) || budget.used != 16 {
		t.Fatalf("exhausted budget accepted another stream: used=%d err=%v", budget.used, err)
	}
}

func TestDownloadBudgetReconcilesActualDiskAndResultWithoutDoubleCharging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download")
	if err := os.WriteFile(path, []byte("12345678"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, reported := range []int64{0, 4, 8} {
		budget := &batchDownloadBudget{limit: 16}
		attempt, err := budget.begin(8)
		if err != nil {
			t.Fatal(err)
		}
		_ = attempt.record(4)
		if err := attempt.reconcile(path, reported, true); err != nil || budget.used != 8 || attempt.bytes() != 8 {
			t.Fatalf("actual file did not own accounting: used=%d bytes=%d err=%v", budget.used, attempt.bytes(), err)
		}
	}
}

func TestDownloadBudgetRejectsUnsafeOrUnknownAccounting(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing"), filepath.Join(root, "loop")} {
		if path == filepath.Join(root, "loop") {
			if err := os.Symlink(path, path); err != nil {
				t.Fatal(err)
			}
		}
		attempt, err := (&batchDownloadBudget{limit: 8}).begin(8)
		if err != nil {
			t.Fatal(err)
		}
		if err := attempt.reconcile(path, 0, true); !errors.Is(err, connectors.ErrTransferByteLimit) {
			t.Fatalf("unverified successful staging accepted: path=%q err=%v", path, err)
		}
	}
}

func TestDownloadBudgetMeasurementsAreOverflowSafeAndSticky(t *testing.T) {
	for _, measured := range []int64{-1, 9, math.MaxInt64} {
		budget := &batchDownloadBudget{limit: 8}
		attempt, err := budget.begin(8)
		if err != nil {
			t.Fatal(err)
		}
		if err := attempt.record(measured); !errors.Is(err, connectors.ErrTransferByteLimit) {
			t.Fatalf("invalid measurement accepted: measured=%d err=%v", measured, err)
		}
		if err := attempt.record(0); !errors.Is(err, connectors.ErrTransferByteLimit) || budget.used < 0 || budget.used > 8 {
			t.Fatalf("failure reset or overflowed: used=%d err=%v", budget.used, err)
		}
	}
	budget := &batchDownloadBudget{limit: math.MaxInt64}
	attempt, err := budget.begin(math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.record(math.MaxInt64); err != nil || budget.used != math.MaxInt64 {
		t.Fatalf("exact maximum rejected or overflowed: used=%d err=%v", budget.used, err)
	}
	var missing *batchDownloadBudget
	if _, err := missing.begin(8); !errors.Is(err, connectors.ErrTransferByteLimit) {
		t.Fatalf("missing budget did not fail closed: %v", err)
	}
}

func TestDownloadBudgetConcurrentProgressUsesOneHighWater(t *testing.T) {
	budget := &batchDownloadBudget{limit: 100}
	attempt, err := budget.begin(100)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for bytes := int64(1); bytes <= 100; bytes++ {
		wait.Add(1)
		go func() { defer wait.Done(); _ = attempt.record(bytes) }()
	}
	wait.Wait()
	if budget.used != 100 || attempt.bytes() != 100 {
		t.Fatalf("concurrent progress duplicated/lost bytes: used=%d observed=%d", budget.used, attempt.bytes())
	}
}

func TestDownloadBudgetInitializationCountsPriorEvidenceAndOwnedFiles(t *testing.T) {
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data.aipdb"), MaxObjectBytes: 8, MaxBatchBytes: 16})
	runtime := &Runtime{storageID: "fixture"}
	path, err := runner.ReserveDownloadTempFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	batch := filetransfer.BatchRecord{Direction: filetransfer.DirectionDownload, Items: []filetransfer.Record{
		{TempPath: path, TransferredBytes: 2}, {TransferredBytes: 3},
	}}
	budget, err := runner.downloadBudget(runtime, batch)
	if err != nil || budget.used != 9 {
		t.Fatalf("existing consumption lost: budget=%#v err=%v", budget, err)
	}
	batch.Items[0].TempPath = filepath.Join(t.TempDir(), "foreign")
	if _, err := runner.downloadBudget(runtime, batch); !errors.Is(err, connectors.ErrTransferByteLimit) {
		t.Fatalf("foreign staging accepted: %v", err)
	}
	if budget, err := runner.downloadBudget(runtime, filetransfer.BatchRecord{Direction: filetransfer.DirectionUpload}); err != nil || budget != nil {
		t.Fatalf("download budget changed upload behavior: budget=%#v err=%v", budget, err)
	}
}
