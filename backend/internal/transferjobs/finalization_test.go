package transferjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type finalizationStoreStub struct {
	completeErrors []error
	completeCalls  int
	record         filetransfer.Record
	syncError      error
	failureKind    string
	failureMessage string
}

type batchFinalizationStoreStub struct {
	completeErrors []error
	completeCalls  int
	record         filetransfer.BatchRecord
	failureKind    string
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

	completed, err := FinalizeSuccessfulFileTransfer(context.Background(), store, 9, 42, "checksum")
	if err == nil || !completed {
		t.Fatalf("completed=%v err=%v", completed, err)
	}
	if store.failureKind != "" {
		t.Fatalf("completed transfer was reclassified as %q", store.failureKind)
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
