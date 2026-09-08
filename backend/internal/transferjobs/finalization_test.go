package transferjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type finalizationStoreStub struct {
	completeErrors []error
	completeCalls  int
	record         filetransfer.Record
	syncError      error
	failureKind    string
	failureMessage string
	failureErrors  []error
	failureCalls   int
}

type batchFinalizationStoreStub struct {
	completeErrors []error
	completeCalls  int
	record         filetransfer.BatchRecord
	failureKind    string
	failureErrors  []error
	failureCalls   int
}

type batchPreparationStoreStub struct {
	recalculateErrors []error
	recalculateCalls  int
	record            filetransfer.BatchRecord
}

func (s *batchPreparationStoreStub) RecalculateBatch(context.Context, int64) error {
	index := s.recalculateCalls
	s.recalculateCalls++
	if index < len(s.recalculateErrors) {
		return s.recalculateErrors[index]
	}
	return nil
}

func (s *batchPreparationStoreStub) GetBatch(context.Context, int64) (filetransfer.BatchRecord, error) {
	return s.record, nil
}

func (s *batchFinalizationStoreStub) CompleteBatch(context.Context, int64) (bool, error) {
	index := s.completeCalls
	s.completeCalls++
	if index < len(s.completeErrors) && s.completeErrors[index] != nil {
		return false, s.completeErrors[index]
	}
	s.record.Status = filetransfer.StatusCompleted
	return true, nil
}

func (s *batchFinalizationStoreStub) GetBatch(context.Context, int64) (filetransfer.BatchRecord, error) {
	return s.record, nil
}

func (s *batchFinalizationStoreStub) FailBatchWithKind(_ context.Context, _ int64, _ string, kind string) (bool, error) {
	index := s.failureCalls
	s.failureCalls++
	if index < len(s.failureErrors) && s.failureErrors[index] != nil {
		return false, s.failureErrors[index]
	}
	if fileTransferStatusTerminal(s.record.Status) {
		return false, nil
	}
	s.failureKind = kind
	s.record.Status = filetransfer.StatusFailed
	return true, nil
}

func (s *finalizationStoreStub) Complete(context.Context, int64, int64, string) (bool, error) {
	index := s.completeCalls
	s.completeCalls++
	if index < len(s.completeErrors) && s.completeErrors[index] != nil {
		return false, s.completeErrors[index]
	}
	s.record.Status = filetransfer.StatusCompleted
	return true, nil
}

func (s *finalizationStoreStub) Get(context.Context, int64) (filetransfer.Record, error) {
	return s.record, nil
}

func (s *finalizationStoreStub) SyncHistory(context.Context, int64) error {
	return s.syncError
}

func (s *finalizationStoreStub) FailWithKind(_ context.Context, _ int64, message string, kind string) (bool, error) {
	index := s.failureCalls
	s.failureCalls++
	if index < len(s.failureErrors) && s.failureErrors[index] != nil {
		return false, s.failureErrors[index]
	}
	if fileTransferStatusTerminal(s.record.Status) {
		return false, nil
	}
	s.failureMessage = message
	s.failureKind = kind
	s.record.Status = filetransfer.StatusFailed
	return true, nil
}

func TestFinalizeSuccessfulFileTransferAcceptsCanonicalCompletionAfterProjectionFailure(t *testing.T) {
	store := &finalizationStoreStub{
		completeErrors: []error{errors.New("history projection unavailable")},
		record:         filetransfer.Record{Status: filetransfer.StatusCompleted},
	}

	completed, err := FinalizeSuccessfulFileTransfer(context.Background(), store, 7, 42, "checksum")
	if err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if store.failureKind != "" {
		t.Fatalf("completed transfer was reclassified as %q", store.failureKind)
	}
}

func TestFinalizeSuccessfulFileTransferClassifiesUnconfirmedOutcome(t *testing.T) {
	store := &finalizationStoreStub{
		completeErrors: []error{errors.New("database unavailable"), errors.New("database unavailable"), errors.New("database unavailable")},
		record:         filetransfer.Record{Status: filetransfer.StatusRunning},
	}

	completed, err := FinalizeSuccessfulFileTransfer(context.Background(), store, 8, 42, "checksum")
	if err == nil || completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if store.completeCalls != fileTransferFinalizationAttempts {
		t.Fatalf("complete calls=%d want=%d", store.completeCalls, fileTransferFinalizationAttempts)
	}
	if store.failureKind != filetransfer.FailureKindOutcomeUnknown {
		t.Fatalf("failure kind=%q", store.failureKind)
	}
	if store.failureMessage != fileTransferOutcomeUnknownMessage {
		t.Fatalf("failure message=%q", store.failureMessage)
	}
}

func TestFinalizeSuccessfulFileTransferDoesNotUndoCompletedCanonicalState(t *testing.T) {
	store := &finalizationStoreStub{
		completeErrors: []error{errors.New("projection failed"), errors.New("projection failed"), errors.New("projection failed")},
		record:         filetransfer.Record{Status: filetransfer.StatusCompleted},
		syncError:      errors.New("projection still unavailable"),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
	defer cancel()
	completed, err := FinalizeSuccessfulFileTransfer(ctx, store, 9, 42, "checksum")
	if err == nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if store.failureKind != "" {
		t.Fatalf("completed transfer was reclassified as %q", store.failureKind)
	}
}

func TestFinalizeSuccessfulFileTransferKeepsRetryingUntilTerminalStateIsDurable(t *testing.T) {
	databaseUnavailable := errors.New("database unavailable")
	store := &finalizationStoreStub{
		completeErrors: []error{databaseUnavailable, databaseUnavailable, databaseUnavailable, databaseUnavailable},
		failureErrors:  []error{databaseUnavailable},
		record:         filetransfer.Record{Status: filetransfer.StatusRunning},
	}

	completed, err := FinalizeSuccessfulFileTransfer(t.Context(), store, 12, 42, "checksum")
	if err != nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if store.completeCalls != 5 || store.failureCalls != 1 {
		t.Fatalf("complete calls=%d failure calls=%d", store.completeCalls, store.failureCalls)
	}
}

func TestPersistTerminalRetriesUntilTheTransitionIsDurable(t *testing.T) {
	attempts := 0
	status := filetransfer.StatusRunning
	durable := PersistTerminal(t.Context(), "file transfer", 14, func(context.Context) (bool, error) {
		attempts++
		if attempts == 1 {
			return false, errors.New("database busy")
		}
		status = filetransfer.StatusFailed
		return true, nil
	}, func(context.Context) (string, error) {
		return status, nil
	})
	if !durable || attempts != 2 || status != filetransfer.StatusFailed {
		t.Fatalf("durable=%v attempts=%d status=%q", durable, attempts, status)
	}
}

func TestPersistTerminalReportsCancellationBeforeDurability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	durable := PersistTerminal(ctx, "file transfer batch", 15, func(ctx context.Context) (bool, error) {
		return false, ctx.Err()
	}, func(ctx context.Context) (string, error) {
		return "", ctx.Err()
	})
	if durable {
		t.Fatal("canceled finalization reported a durable terminal state")
	}
}

func TestPersistTerminalAcceptsAnExistingTerminalState(t *testing.T) {
	durable := PersistTerminal(t.Context(), "file transfer batch", 16, func(context.Context) (bool, error) {
		return false, nil
	}, func(context.Context) (string, error) {
		return filetransfer.StatusCanceled, nil
	})
	if !durable {
		t.Fatal("existing terminal state was not recognized as durable")
	}
}

func TestFinalizeFileTransferBatchRetriesTransientPersistenceFailure(t *testing.T) {
	store := &batchFinalizationStoreStub{
		completeErrors: []error{errors.New("database busy")},
		record:         filetransfer.BatchRecord{Status: filetransfer.StatusRunning},
	}
	if err := FinalizeFileTransferBatch(context.Background(), store, 10); err != nil {
		t.Fatalf("finalize batch: %v", err)
	}
	if store.completeCalls != 2 || store.failureKind != "" {
		t.Fatalf("complete calls=%d failure kind=%q", store.completeCalls, store.failureKind)
	}
}

func TestFinalizeFileTransferBatchClassifiesLocalPersistenceFailure(t *testing.T) {
	store := &batchFinalizationStoreStub{
		completeErrors: []error{errors.New("database busy"), errors.New("database busy"), errors.New("database busy")},
		record:         filetransfer.BatchRecord{Status: filetransfer.StatusRunning},
	}
	if err := FinalizeFileTransferBatch(context.Background(), store, 11); err == nil {
		t.Fatal("expected batch finalization error")
	}
	if store.failureKind != filetransfer.FailureKindLocalPersistence {
		t.Fatalf("failure kind=%q", store.failureKind)
	}
}

func TestFinalizeFileTransferBatchKeepsRetryingUntilTerminalStateIsDurable(t *testing.T) {
	databaseUnavailable := errors.New("database unavailable")
	store := &batchFinalizationStoreStub{
		completeErrors: []error{databaseUnavailable, databaseUnavailable, databaseUnavailable, databaseUnavailable},
		failureErrors:  []error{databaseUnavailable},
		record:         filetransfer.BatchRecord{Status: filetransfer.StatusRunning},
	}

	if err := FinalizeFileTransferBatch(t.Context(), store, 13); err != nil {
		t.Fatalf("finalize batch: %v", err)
	}
	if store.completeCalls != 5 || store.failureCalls != 1 {
		t.Fatalf("complete calls=%d failure calls=%d", store.completeCalls, store.failureCalls)
	}
}

func TestPrepareFileTransferBatchRetriesAggregatePersistence(t *testing.T) {
	databaseUnavailable := errors.New("database unavailable")
	store := &batchPreparationStoreStub{
		recalculateErrors: []error{databaseUnavailable, databaseUnavailable, databaseUnavailable, databaseUnavailable},
		record:            filetransfer.BatchRecord{ID: 15, Status: filetransfer.StatusRunning, CompletedItems: 2},
	}

	batch, err := PrepareFileTransferBatch(t.Context(), store, 15)
	if err != nil {
		t.Fatal(err)
	}
	if store.recalculateCalls != 5 || batch.ID != 15 || batch.CompletedItems != 2 {
		t.Fatalf("calls=%d batch=%#v", store.recalculateCalls, batch)
	}
}
