package commandrequests

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type testCommandCodec struct{}

func (testCommandCodec) Seal(_ int64, command string) (string, error) {
	return "sealed:" + command, nil
}
func (testCommandCodec) Open(_ int64, sealed string) (string, error) {
	return strings.TrimPrefix(sealed, "sealed:"), nil
}

type testCommandProjection struct {
	ids []int64
	err error
}

func (p *testCommandProjection) SyncCommandRequest(_ context.Context, _ Executor, id int64) error {
	p.ids = append(p.ids, id)
	return p.err
}

func TestStoreInsertSealsRawCommandAndRollsBackProjectionFailure(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	store := NewStore(database)
	request := PreparedInsert{
		insert:        Insert{RuntimeID: runtimeID, Source: SourceManual, Command: "secret raw", Status: "running"},
		storedCommand: "[REDACTED]", storedReason: "safe reason",
	}
	projectionFailure := errors.New("projection failed")
	if _, err := store.Insert(t.Context(), testCommandCodec{}, &testCommandProjection{err: projectionFailure}, request); !errors.Is(err, projectionFailure) {
		t.Fatalf("Insert() error = %v", err)
	}
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM command_requests`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back command requests = %d", count)
	}

	projection := &testCommandProjection{}
	id, err := store.Insert(t.Context(), testCommandCodec{}, projection, request)
	if err != nil {
		t.Fatal(err)
	}
	var display, sealed string
	if err := database.QueryRowContext(t.Context(), `
		SELECT command, encrypted_command FROM command_requests WHERE id = ?`, id,
	).Scan(&display, &sealed); err != nil {
		t.Fatal(err)
	}
	if display != "[REDACTED]" || sealed != "sealed:secret raw" {
		t.Fatalf("display=%q sealed=%q", display, sealed)
	}
	command, err := store.ExecutionCommand(t.Context(), testCommandCodec{}, id)
	if err != nil || command != "secret raw" {
		t.Fatalf("ExecutionCommand() = %q, %v", command, err)
	}
}

func TestStoreFinishesAndCancelsOnlyRequestedScopes(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	store := NewStore(database)
	projection := &testCommandProjection{}
	insert := func(sessionID int64) int64 {
		t.Helper()
		id, err := store.Insert(t.Context(), testCommandCodec{}, projection, PreparedInsert{
			insert:        Insert{RuntimeID: runtimeID, Command: "sleep", Status: "running"},
			storedCommand: "sleep",
		})
		if err != nil {
			t.Fatal(err)
		}
		if sessionID > 0 {
			if err := store.SetSession(t.Context(), projection, id, sessionID); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	finishedID := insert(11)
	sessionCanceledID := insert(22)
	runtimeCanceledID := insert(33)
	if err := store.Finish(t.Context(), projection, Completion{
		ID: finishedID, Status: "completed", SessionID: 11, Stdout: "done", ExitCode: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CancelRunningForSession(t.Context(), projection, 22, "session closed"); err != nil {
		t.Fatal(err)
	}
	affected, err := store.CancelRunningForRuntime(t.Context(), projection, runtimeID, "runtime closed")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("runtime canceled = %d", affected)
	}
	for id, expected := range map[int64]string{
		finishedID: "completed", sessionCanceledID: "error", runtimeCanceledID: "error",
	} {
		var status string
		if err := database.QueryRowContext(t.Context(), `SELECT status FROM command_requests WHERE id = ?`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != expected {
			t.Fatalf("request %d status = %q, want %q", id, status, expected)
		}
	}
}

func TestStoreRollsBackCompletionWhenProjectionFails(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	store := NewStore(database)
	id, err := store.Insert(t.Context(), testCommandCodec{}, &testCommandProjection{}, PreparedInsert{
		insert: Insert{RuntimeID: runtimeID, Command: "sleep", Status: "running"}, storedCommand: "sleep",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectionFailure := errors.New("projection failed")
	err = store.Finish(t.Context(), &testCommandProjection{err: projectionFailure}, Completion{
		ID: id, Status: "completed", Stdout: "done",
	})
	if !errors.Is(err, projectionFailure) {
		t.Fatalf("Finish() error = %v", err)
	}
	var status, stdout string
	if err := database.QueryRowContext(t.Context(), `SELECT status, stdout FROM command_requests WHERE id = ?`, id).Scan(&status, &stdout); err != nil {
		t.Fatal(err)
	}
	if status != "running" || stdout != "" {
		t.Fatalf("completion escaped rollback: status=%q stdout=%q", status, stdout)
	}
}

func TestStoreRollsBackCancellationWhenProjectionFails(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	store := NewStore(database)
	id, err := store.Insert(t.Context(), testCommandCodec{}, &testCommandProjection{}, PreparedInsert{
		insert: Insert{RuntimeID: runtimeID, Command: "sleep", Status: "running"}, storedCommand: "sleep",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectionFailure := errors.New("projection failed")
	err = store.CancelRunning(t.Context(), &testCommandProjection{err: projectionFailure}, "workspace closed")
	if !errors.Is(err, projectionFailure) {
		t.Fatalf("CancelRunning() error = %v", err)
	}
	var status, errorText string
	if err := database.QueryRowContext(t.Context(), `SELECT status, error FROM command_requests WHERE id = ?`, id).Scan(&status, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != "running" || errorText != "" {
		t.Fatalf("cancellation escaped rollback: status=%q error=%q", status, errorText)
	}
}

func TestStoreRejectsMissingDependencies(t *testing.T) {
	if _, err := NewStore(nil).Insert(t.Context(), testCommandCodec{}, &testCommandProjection{}, PreparedInsert{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Insert() error = %v", err)
	}
	database, _ := commandRequestFixture(t)
	if _, err := NewStore(database).Insert(t.Context(), nil, &testCommandProjection{}, PreparedInsert{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Insert() codec error = %v", err)
	}
}

var _ Projection = (*testCommandProjection)(nil)
var _ CommandCodec = testCommandCodec{}
var _ Executor = (*sql.DB)(nil)
var _ Database = (*sql.DB)(nil)
