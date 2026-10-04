package uploadoperation_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups/uploadoperation"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type journalFaultExecutor struct {
	sqldb.Executor
	err    error
	result sql.Result
}

func (e journalFaultExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return e.result, e.err
}

type journalFaultResult struct{ err error }

func (r journalFaultResult) LastInsertId() (int64, error) { return 0, r.err }
func (r journalFaultResult) RowsAffected() (int64, error) { return 0, r.err }

func TestUploadJournalPreservesStorageFailureCauses(t *testing.T) {
	database, _, request := journalClaim(t)
	cause := errors.New("fixture storage failure")
	executor := journalFaultExecutor{Executor: database, err: cause}
	store := uploadoperation.NewStore(executor)
	for name, mutation := range map[string]func() error{
		"claim":             func() error { _, _, err := store.Claim(t.Context(), request); return err },
		"dispatch":          func() error { return store.MarkDispatched(t.Context(), request.IdempotencyKey) },
		"complete":          func() error { return store.Complete(t.Context(), request.IdempotencyKey, "result") },
		"expire unresolved": func() error { return store.ExpireUnresolved(t.Context(), request.ProviderID, "changed") },
		"expire result":     func() error { return store.ExpireResult(t.Context(), request.ProviderID, "result", "now") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutation(); !errors.Is(err, cause) {
				t.Fatalf("storage cause lost: %v", err)
			}
		})
	}
	if _, err := database.ExecContext(t.Context(), "DELETE FROM backup_upload_operations"); err != nil {
		t.Fatal(err)
	}
	executor.err = nil
	executor.result = journalFaultResult{err: cause}
	store = uploadoperation.NewStore(executor)
	for _, mutation := range []func() error{
		func() error { _, _, err := store.Claim(t.Context(), request); return err },
		func() error { return store.MarkDispatched(t.Context(), request.IdempotencyKey) },
		func() error { return store.Complete(t.Context(), request.IdempotencyKey, "result") },
	} {
		if err := mutation(); !errors.Is(err, cause) {
			t.Fatalf("RowsAffected cause lost: %v", err)
		}
	}
	store = uploadoperation.NewStore(journalFaultExecutor{Executor: database, result: journalFaultResult{}})
	if _, _, err := store.Claim(t.Context(), request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row after claim lost: %v", err)
	}
	if _, err := uploadoperation.NewStore(database).Get(t.Context(), request.IdempotencyKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed mutations persisted a journal row: %v", err)
	}
}

func TestUploadJournalUnknownMessageAndRepeatedExpiry(t *testing.T) {
	_, store, request := journalClaim(t)
	if _, _, err := store.Claim(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkOutcomeUnknown(t.Context(), request.IdempotencyKey, errors.New("reply lost")); err != nil {
		t.Fatal(err)
	}
	unknown, err := store.Get(t.Context(), request.IdempotencyKey)
	if err != nil || unknown.Status != "outcome_unknown" || unknown.LastError != "reply lost" || unknown.CompletedAt != nil {
		t.Fatalf("unknown finality: %#v err=%v", unknown, err)
	}
	for range 2 {
		if err := store.MarkExpired(t.Context(), request.IdempotencyKey); err != nil {
			t.Fatal(err)
		}
	}
	expired, err := store.Get(t.Context(), request.IdempotencyKey)
	if err != nil || expired.Status != "expired" || expired.CompletedAt == nil || !strings.Contains(expired.LastError, "expired") {
		t.Fatalf("expired finality: %#v err=%v", expired, err)
	}
}
