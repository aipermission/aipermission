package console

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	consolepersistence "github.com/aipermission/aipermission/backend/internal/console/persistence"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

func TestConsolePersistenceFailureDoesNotGrowPendingWithoutBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	manager := NewManager(nil, nil, nil)
	manager.db = &sql.DB{}
	manager.persistChunks = func(context.Context, *sql.DB, int64, string, string, string) error {
		return errors.New("storage unavailable")
	}
	session := &managedConsoleSession{ctx: ctx, cancel: cancel, manager: manager,
		clients: map[*websocket.Conn]*sync.Mutex{}, workClosed: true}
	var appendErr error
	for range 32 {
		if appendErr = session.appendSafeOutput(strings.Repeat("x", maxConsoleChunkLength)); appendErr != nil {
			break
		}
	}
	bytes := session.outputBuffer.Len()
	if bytes != consolepersistence.MaxPendingBytes || !errors.Is(appendErr, context.DeadlineExceeded) {
		t.Fatalf("pending=%d producer error=%v", bytes, appendErr)
	}
}

func TestConsoleTranscriptRetriesIntoSQLCipherWithoutAnotherAppend(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	session.ctx, session.cancel = context.WithCancel(t.Context())
	t.Cleanup(func() {
		session.cancel()
		session.drainOwnedWork()
	})
	var attempts atomic.Int32
	persisted := make(chan struct{})
	manager.persistChunks = func(ctx context.Context, database *sql.DB, id int64, snapshot, pending, now string) error {
		if attempts.Add(1) == 1 {
			return errors.New("injected transient database failure")
		}
		err := consolepersistence.PersistTranscript(ctx, database, id, snapshot, pending, now)
		if err == nil {
			close(persisted)
		}
		return err
	}
	const output = "accepted exactly once after retry\r\n"
	if err := session.appendSafeOutput(output); err != nil {
		t.Fatal(err)
	}
	select {
	case <-persisted:
	case <-time.After(3 * time.Second):
		t.Fatal("transient failure did not retry autonomously")
	}
	session.cancel()
	session.drainOwnedWork()
	var count int
	var stored string
	if err := database.QueryRow(`SELECT COUNT(*), COALESCE(group_concat(data, ''), '') FROM console_session_chunks WHERE session_id = ?`, session.id).Scan(&count, &stored); err != nil {
		t.Fatal(err)
	}
	if count != 1 || stored != output || session.outputBuffer.Len() != 0 || attempts.Load() != 2 {
		t.Fatalf("count=%d output=%q pending=%d attempts=%d", count, stored, session.outputBuffer.Len(), attempts.Load())
	}
}

func TestConsoleFullPendingBufferDoesNotHideUnacceptedRedactorTail(t *testing.T) {
	_, _, session := newManualHistoryTestSession(t)
	session.workClosed = true
	session.ctx, session.cancel = context.WithCancel(t.Context())
	session.cancel()
	if err := session.outputBuffer.Append(t.Context(), strings.Repeat("x", consolepersistence.MaxPendingBytes), nil); err != nil {
		t.Fatal(err)
	}
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE", Value: []byte("fixture-secret-tail")}})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Destroy()
	session.stdoutExactRedactor, err = environment.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	if output := session.stdoutExactRedactor.Write([]byte("fixture-se")); len(output) != 0 {
		t.Fatalf("fixture prefix was not buffered: %q", output)
	}
	session.finalStatus = "closed"
	session.closeExactRedactor()
	if session.finalStatus != "error" || !strings.Contains(session.finalMessage, "incomplete") {
		t.Fatalf("unaccepted final tail reported successful closure: %q %q", session.finalStatus, session.finalMessage)
	}
	if session.outputBuffer.Len() != consolepersistence.MaxPendingBytes {
		t.Fatal("final tail overflow escaped capacity")
	}
}

func TestConsolePostDispatchPersistenceBackpressureRetainsReconciliation(t *testing.T) {
	manager, session := newCancellationTestSession(t)
	session.workClosed = true
	if err := session.outputBuffer.Append(t.Context(), strings.Repeat("x", consolepersistence.MaxPendingBytes), nil); err != nil {
		t.Fatal(err)
	}
	// Call the session directly: this fixture isolates post-write observation,
	// rather than closed work admission in the manager's authorizer.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	writer := &cancellationWriter{}
	session.stdin = writer
	result, err := session.execCommand(ctx, "printf 'dispatched-with-full-transcript'", nil)
	if !errors.Is(err, ErrCommandOutcomeUnknown) || !result.Running || result.SessionID != session.id || result.Generation != session.generation {
		t.Fatalf("post-dispatch pressure lost reconciliation: %#v %v", result, err)
	}
	if len(writer.snapshot()) != 2 || session.activeCommand() == nil || manager.active(session.id) != session {
		t.Fatal("post-dispatch pressure erased dispatch evidence")
	}
}

func TestConsoleDisplayObserverCanCancelBehindBlockedProducer(t *testing.T) {
	_, _, session := newManualHistoryTestSession(t)
	session.ctx, session.cancel = context.WithCancel(t.Context())
	defer session.cancel()
	session.workClosed = true
	if err := session.outputBuffer.Append(t.Context(), strings.Repeat("x", consolepersistence.MaxPendingBytes), nil); err != nil {
		t.Fatal(err)
	}
	producer := make(chan error, 1)
	go func() { producer <- session.appendSafeOutput("blocked-producer") }()
	waitConsoleTranscript(t, session, "blocked-producer")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := session.appendDisplayOutput(ctx, "canceled observer"); !errors.Is(err, context.Canceled) {
		t.Fatalf("observer did not honor cancellation behind producer: %v", err)
	}
	_, snapshot := session.snapshot()
	if strings.Contains(snapshot, "canceled observer") {
		t.Fatal("canceled observer changed transcript while waiting for delivery ownership")
	}
	session.cancel()
	if err := <-producer; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestUnfinalizedFailedSessionsStillConsumeCapacity(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	for id := int64(1); id <= maxActiveConsoleSessions; id++ {
		manager.sessions[id] = &managedConsoleSession{status: "closed", finalStatus: "closed", manager: manager}
	}
	manager.mu.Lock()
	count := manager.activeSessionCountLocked()
	manager.mu.Unlock()
	if count != maxActiveConsoleSessions {
		t.Fatalf("failed finalizations escaped the session limit: %d", count)
	}
	if _, err := manager.Create(t.Context(), CreateRequest{RuntimeID: 1, Principal: testExecutionPrincipal()}); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("failed finalizations admitted another session: %v", err)
	}
}
