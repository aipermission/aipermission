package transferjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type evidenceStoreStub struct {
	record filetransfer.Record
	update func(context.Context, int64, int64, string) error
	read   func(context.Context, int64) (filetransfer.Record, error)
	calls  int
}

func (store *evidenceStoreStub) UpdateEvidence(ctx context.Context, id, size int64, checksum string) error {
	store.calls++
	if store.update != nil {
		return store.update(ctx, id, size, checksum)
	}
	store.record.TransferredBytes = max(store.record.TransferredBytes, size)
	if checksum != "" {
		store.record.ChecksumSHA256 = checksum
	}
	return nil
}

func (store *evidenceStoreStub) Get(ctx context.Context, id int64) (filetransfer.Record, error) {
	if store.read != nil {
		return store.read(ctx, id)
	}
	return store.record, nil
}

func TestPersistFileTransferEvidencePreservesTerminalState(t *testing.T) {
	for _, status := range []string{filetransfer.StatusRunning, filetransfer.StatusCanceled, filetransfer.StatusFailed, filetransfer.StatusCompleted} {
		t.Run(status, func(t *testing.T) {
			store := &evidenceStoreStub{record: filetransfer.Record{Status: status, TransferredBytes: 12, ChecksumSHA256: "old"}}
			if err := PersistFileTransferEvidence(t.Context(), store, 1, 6, " new "); err != nil {
				t.Fatal(err)
			}
			if store.record.Status != status || store.record.TransferredBytes != 12 || store.record.ChecksumSHA256 != "new" || store.calls != 1 {
				t.Fatalf("record=%#v calls=%d", store.record, store.calls)
			}
		})
	}
}

func TestPersistFileTransferEvidenceRetriesUnconfirmedWrites(t *testing.T) {
	for _, failure := range []string{"update", "read", "bytes", "checksum"} {
		t.Run(failure, func(t *testing.T) {
			store := &evidenceStoreStub{}
			store.update = func(_ context.Context, id, size int64, checksum string) error {
				if store.calls == 1 && failure == "update" {
					return errors.New("busy")
				}
				store.record = filetransfer.Record{ID: id, TransferredBytes: size, ChecksumSHA256: checksum}
				return nil
			}
			store.read = func(context.Context, int64) (filetransfer.Record, error) {
				if store.calls == 1 {
					switch failure {
					case "read":
						return filetransfer.Record{}, errors.New("unavailable")
					case "bytes":
						return filetransfer.Record{TransferredBytes: 5, ChecksumSHA256: "hash"}, nil
					case "checksum":
						return filetransfer.Record{TransferredBytes: 6}, nil
					}
				}
				return store.record, nil
			}
			if err := PersistFileTransferEvidence(t.Context(), store, 1, 6, "hash"); err != nil || store.calls != 2 {
				t.Fatalf("calls=%d err=%v", store.calls, err)
			}
		})
	}
}

func TestPersistFileTransferEvidenceStopsWithoutPretendingDurability(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	diskFailure := errors.New("disk failure")
	store := &evidenceStoreStub{update: func(context.Context, int64, int64, string) error {
		cancel()
		return diskFailure
	}}
	err := PersistFileTransferEvidence(ctx, store, 1, 6, "")
	if !errors.Is(err, ErrEvidenceNotDurable) || !errors.Is(err, context.Canceled) || !errors.Is(err, diskFailure) || store.calls != 1 {
		t.Fatalf("calls=%d err=%v", store.calls, err)
	}
	if err := PersistFileTransferEvidence(ctx, store, 1, 6, ""); !errors.Is(err, context.Canceled) || store.calls != 1 {
		t.Fatalf("canceled context dispatched write: calls=%d err=%v", store.calls, err)
	}
}

func TestPersistFileTransferEvidenceRejectsInvalidIdentityAndBytes(t *testing.T) {
	store := &evidenceStoreStub{}
	for _, input := range []struct{ id, bytes int64 }{{0, 1}, {-1, 1}, {1, -1}} {
		err := PersistFileTransferEvidence(t.Context(), store, input.id, input.bytes, "")
		if !errors.Is(err, ErrEvidenceNotDurable) || !errors.Is(err, filetransfer.ErrInvalidArgument) || store.calls != 0 {
			t.Fatalf("input=%#v calls=%d err=%v", input, store.calls, err)
		}
	}
	if err := PersistFileTransferEvidence(t.Context(), nil, 1, 1, ""); !errors.Is(err, filetransfer.ErrInvalidArgument) {
		t.Fatal(err)
	}
}
