package console

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

func testRuntimeDone(wait func() error) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- wait()
		close(done)
	}()
	return done
}

func testRuntimeOutput(chunks ...string) <-chan RuntimeOutput {
	output := make(chan RuntimeOutput, len(chunks))
	for _, chunk := range chunks {
		output <- RuntimeOutput{Kind: RuntimeStdout, Data: chunk}
	}
	close(output)
	return output
}

func TestConsoleSessionManagerCreateValidationAndCloseInactive(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	manager := NewManager(database, nil, nil)

	if _, err := manager.Create(context.Background(), CreateRequest{Principal: testExecutionPrincipal()}); err == nil {
		t.Fatalf("expected missing server id to fail")
	}
	if err := manager.Close(context.Background(), testExecutionPrincipal(), 999); err != nil {
		t.Fatalf("closing inactive/missing session should be idempotent: %v", err)
	}
}

func TestConsoleSessionManagerCloseAllDrainsSessionsAndRejectsLateCreates(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-close-all", "127.0.0.1", 22)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin:  &recordingWriteCloser{},
			Output: testRuntimeOutput("closing output"),
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	if _, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
	}); err != nil {
		t.Fatalf("create active session: %v", err)
	}

	if err := manager.CloseAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count := manager.activeSessionCount(); count != 0 {
		t.Fatalf("sessions retained after CloseAll: %d", count)
	}
	if _, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "late", Principal: testExecutionPrincipal(),
	}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("late create error = %v, want %v", err, ErrManagerClosed)
	}
}

func TestConsoleSessionManagerImplicitSessionCreationIsSingleFlight(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-single-flight", "127.0.0.1", 22)
	output := make(chan RuntimeOutput)
	var opens atomic.Int32
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		opens.Add(1)
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: output,
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	t.Cleanup(func() {
		_ = manager.CloseAll(context.Background())
	})

	const callers = 12
	start := make(chan struct{})
	handles := make(chan SessionHandle, callers)
	resultErrors := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			handle, ensureErr := manager.EnsureReady(t.Context(), testExecutionPrincipal(), runtimeID)
			handles <- handle
			resultErrors <- ensureErr
		}()
	}
	close(start)
	var first SessionHandle
	for range callers {
		if ensureErr := <-resultErrors; ensureErr != nil {
			t.Fatal(ensureErr)
		}
		handle := <-handles
		if !first.Valid() {
			first = handle
		} else if handle != first {
			t.Fatalf("implicit session handles differ: first=%#v current=%#v", first, handle)
		}
	}
	if opens.Load() != 1 {
		t.Fatalf("runtime opens = %d, want 1", opens.Load())
	}
	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_sessions WHERE runtime_id = ?`, runtimeID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("persisted implicit sessions = %d, want 1", rows)
	}
}

func TestConsoleSessionManagerSerializesGlobalSessionAdmission(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeIDs := []int64{
		insertConsoleTestSSHProfile(t, database, "worker-admission-a", "127.0.0.1", 22),
		insertConsoleTestSSHProfile(t, database, "worker-admission-b", "127.0.0.1", 22),
	}
	output := make(chan RuntimeOutput)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: output,
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	for index := 0; index < maxActiveConsoleSessions-1; index++ {
		id := int64(index + 10_000)
		manager.sessions[id] = &managedConsoleSession{id: id, runtimeID: id, status: "connected"}
	}
	start := make(chan struct{})
	resultErrors := make(chan error, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		go func() {
			<-start
			_, createErr := manager.Create(t.Context(), CreateRequest{RuntimeID: runtimeID, Principal: testExecutionPrincipal()})
			resultErrors <- createErr
		}()
	}
	close(start)
	successes := 0
	limits := 0
	for range runtimeIDs {
		switch createErr := <-resultErrors; {
		case createErr == nil:
			successes++
		case errors.Is(createErr, ErrSessionLimit):
			limits++
		default:
			t.Fatalf("unexpected create error: %v", createErr)
		}
	}
	if successes != 1 || limits != 1 || manager.activeSessionCount() != maxActiveConsoleSessions {
		t.Fatalf("successes=%d limits=%d active=%d", successes, limits, manager.activeSessionCount())
	}
	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_sessions WHERE runtime_id IN (?, ?)`, runtimeIDs[0], runtimeIDs[1]).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("persisted admitted sessions = %d, want 1", rows)
	}
	var admitted *managedConsoleSession
	for _, runtimeID := range runtimeIDs {
		if session := manager.activeForRuntime(runtimeID); session != nil {
			admitted = session
			break
		}
	}
	if admitted == nil {
		t.Fatal("admitted session is not active")
	}
	admitted.beginClose()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := admitted.waitDone(cleanupCtx); err != nil {
		t.Fatalf("drain admitted session: %v", err)
	}
}

func TestConsoleSessionManagerCloseExistingCannotBypassGlobalLimit(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-close-existing-limit", "127.0.0.1", 22)
	manager := NewManager(database, nil, nil)
	for index := 0; index < maxActiveConsoleSessions; index++ {
		id := int64(index + 20_000)
		manager.sessions[id] = &managedConsoleSession{id: id, runtimeID: id, status: "connected"}
	}

	_, err = manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Principal: testExecutionPrincipal(), CloseExisting: true,
	})
	if !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("create error = %v, want %v", err, ErrSessionLimit)
	}
	if manager.activeSessionCount() != maxActiveConsoleSessions {
		t.Fatalf("active sessions = %d", manager.activeSessionCount())
	}
	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_sessions WHERE runtime_id = ?`, runtimeID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("persisted bypass sessions = %d", rows)
	}
}

func TestConsoleSessionCloseDoesNotWaitForTransportCompletionSignal(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-stalled-completion", "127.0.0.1", 22)
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(),
			Done: make(chan error), Close: func() error { return nil },
		}, nil
	}, nil)
	record, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Close(ctx, testExecutionPrincipal(), record.ID); err != nil {
		t.Fatalf("close with stalled transport completion: %v", err)
	}
}

func TestConsoleSessionManagerBeginCloseAllRevokesAttachedLocalClient(t *testing.T) {
	principal := testExecutionPrincipal()
	ctx, cancel := context.WithCancel(context.Background())
	manager := NewManager(nil, nil, nil)
	session := &managedConsoleSession{
		id: 1, runtimeID: 2, generation: 3, principal: principal, manager: manager,
		ctx: ctx, cancel: cancel, status: "connected", clients: map[*websocket.Conn]*sync.Mutex{},
	}
	manager.sessions[session.id] = session

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = manager.Attach(w, r, principal, session.id, func(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
			return upgrader.Upgrade(w, r, nil)
		})
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial attached client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, _, err := client.ReadMessage(); err != nil {
		t.Fatalf("read initial snapshot: %v", err)
	}

	manager.BeginCloseAll()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := client.ReadMessage(); err == nil {
		t.Fatal("attached client remained open after manager shutdown began")
	}
	called := false
	if err := manager.authorizeOperation(t.Context(), principal, session, OperationInput, func() error {
		called = true
		return nil
	}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("post-shutdown local authorization error = %v, want %v", err, ErrManagerClosed)
	}
	if called {
		t.Fatal("post-shutdown local operation executed")
	}
}

func TestConsoleSessionManagerCloseAllIsBoundedAndClosesTransportOnce(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-bounded-close", "127.0.0.1", 22)
	release := make(chan struct{})
	closeStarted := make(chan struct{})
	var closeOnce sync.Once
	var closeCalls atomic.Int32
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin:  &recordingWriteCloser{},
			Output: testRuntimeOutput(),
			Done:   testRuntimeDone(func() error { <-release; return nil }),
			Close: func() error {
				closeCalls.Add(1)
				closeOnce.Do(func() { close(closeStarted) })
				<-release
				return nil
			},
		}, nil
	}, nil)
	if _, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
	}); err != nil {
		t.Fatalf("create active session: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := manager.CloseAll(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseAll() error = %v, want deadline exceeded", err)
	}
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("transport close did not start after cancellation")
	}
	secondCtx, secondCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer secondCancel()
	if err := manager.CloseAll(secondCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second CloseAll() error = %v, want deadline exceeded", err)
	}
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			concurrentCtx, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer stop()
			if err := manager.CloseAll(concurrentCtx); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("concurrent CloseAll() error = %v", err)
			}
		})
	}
	callers.Wait()
	close(release)
	drainCtx, drainCancel := context.WithTimeout(t.Context(), time.Second)
	defer drainCancel()
	if err := manager.CloseAll(drainCtx); err != nil {
		t.Fatalf("CloseAll() did not observe eventual drain: %v", err)
	}
	if closeCalls.Load() != 1 {
		t.Fatalf("transport Close() calls = %d, want 1", closeCalls.Load())
	}
}

func TestConsoleSessionManagerCloseAllWaitsForTransportAndOwnedPersistence(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-owned-drain", "127.0.0.1", 22)
	output := make(chan RuntimeOutput)
	var closeOnce sync.Once
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: output,
			Done:  testRuntimeDone(func() error { <-ctx.Done(); return ctx.Err() }),
			Close: func() error { closeOnce.Do(func() { close(output) }); return nil },
		}, nil
	}, nil)
	record, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := manager.active(record.ID)
	ownedRelease := make(chan struct{})
	if !session.runOwnedWork(func() { <-ownedRelease }) {
		t.Fatal("owned persistence work was not admitted")
	}

	shortCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := manager.CloseAll(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseAll() error = %v, want blocked ownership deadline", err)
	}
	close(ownedRelease)
	if err := manager.CloseAll(t.Context()); err != nil {
		t.Fatalf("CloseAll() did not observe full ownership drain: %v", err)
	}
}

func TestConsoleSessionClosesTransportBeforeFinishingOutputConsumption(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-close-order", "127.0.0.1", 22)
	output := make(chan RuntimeOutput)
	closed := make(chan struct{})
	var closeOnce sync.Once
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: output,
			Done: make(chan error),
			Close: func() error {
				closeOnce.Do(func() { close(output); close(closed) })
				return nil
			},
		}, nil
	}, nil)
	if _, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "close-order", Principal: testExecutionPrincipal(), WaitForStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.CloseAll(ctx); err != nil {
		t.Fatalf("close after wait: %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("transport was not closed before pipe ownership drained")
	}
}

type closeUnblocksWriteCloser struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (writer *closeUnblocksWriteCloser) Write([]byte) (int, error) {
	writer.startOnce.Do(func() { close(writer.started) })
	<-writer.release
	return 0, io.ErrClosedPipe
}

func (writer *closeUnblocksWriteCloser) Close() error {
	writer.closeOnce.Do(func() { close(writer.release) })
	return nil
}

func TestClosingConnectingSessionUnblocksEnvironmentWriteAndDestroysEnvelope(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-blocked-environment", "127.0.0.1", 22)
	writer := &closeUnblocksWriteCloser{started: make(chan struct{}), release: make(chan struct{})}
	output := make(chan RuntimeOutput)
	done := make(chan error, 1)
	var closeOnce sync.Once
	var closeCalls atomic.Int32
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: writer, Output: output, Done: done, PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(_ context.Context, environment *sessionenv.Envelope) error {
				return environment.ForEach(func(_ string, value []byte, _ bool, _ int64, _ int64, _ int64) error {
					_, writeErr := writer.Write(value)
					return writeErr
				})
			},
			Close: func() error {
				closeCalls.Add(1)
				closeOnce.Do(func() {
					_ = writer.Close()
					close(output)
					done <- context.Canceled
					close(done)
				})
				return nil
			},
		}, nil
	}, nil)
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "API_TOKEN", Value: []byte("blocked-environment-secret"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "blocked environment", Principal: testExecutionPrincipal(), Environment: envelope,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("environment write did not block")
	}
	closeCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Close(closeCtx, testExecutionPrincipal(), record.ID); err != nil {
		t.Fatalf("close connecting session: %v", err)
	}
	if closeCalls.Load() != 1 {
		t.Fatalf("transport close calls = %d, want 1", closeCalls.Load())
	}
	if err := envelope.WithEntries(func([]sessionenv.EntryView) error { return nil }); !errors.Is(err, sessionenv.ErrDestroyed) {
		t.Fatalf("environment remained readable after close: %v", err)
	}
}

func TestClosingBeforeRuntimePublicationClosesPublishedTransportOnce(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-publication-race", "127.0.0.1", 22)
	openStarted := make(chan struct{})
	releaseOpen := make(chan struct{})
	var closeCalls atomic.Int32
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		close(openStarted)
		<-releaseOpen
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(), Done: make(chan error),
			Close: func() error { closeCalls.Add(1); return nil },
		}, nil
	}, nil)
	record, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "publication race", Principal: testExecutionPrincipal(),
	})
	if err != nil {
		t.Fatal(err)
	}
	<-openStarted
	closed := make(chan error, 1)
	go func() { closed <- manager.Close(t.Context(), testExecutionPrincipal(), record.ID) }()
	close(releaseOpen)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close racing publication: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not complete after runtime publication")
	}
	if closeCalls.Load() != 1 {
		t.Fatalf("transport close calls = %d, want 1", closeCalls.Load())
	}
}

func TestConsoleSessionFinalizationRetriesPersistenceAndOwnershipHook(t *testing.T) {
	persistErr := errors.New("injected transcript persistence failure")
	hookErr := errors.New("injected session ownership failure")
	manager := NewManager(nil, nil, nil)
	manager.db = &sql.DB{}
	var transcriptAttempts, statusAttempts, hookAttempts int
	manager.persistChunks = func(context.Context, *sql.DB, int64, string, string, string) error {
		transcriptAttempts++
		if transcriptAttempts == 1 {
			return persistErr
		}
		return nil
	}
	manager.persistStatus = func(context.Context, *sql.DB, int64, string, string, string) error {
		statusAttempts++
		return nil
	}
	manager.SetSessionClosedHook(func(context.Context, SessionHandle) error {
		hookAttempts++
		if hookAttempts == 1 {
			return hookErr
		}
		return nil
	})
	session := &managedConsoleSession{
		id: 41, runtimeID: 7, generation: 3, manager: manager,
		status: "closed", finalStatus: "closed",
	}
	manager.sessions[session.id] = session

	if err := session.finalize(t.Context()); !errors.Is(err, persistErr) {
		t.Fatalf("first finalization error = %v, want transcript failure", err)
	}
	if manager.active(session.id) != session {
		t.Fatal("session was unregistered after failed terminal persistence")
	}
	if err := session.finalize(t.Context()); !errors.Is(err, hookErr) {
		t.Fatalf("second finalization error = %v, want ownership hook failure", err)
	}
	if manager.active(session.id) != session {
		t.Fatal("session was unregistered after failed ownership hook")
	}
	if err := session.finalize(t.Context()); err != nil {
		t.Fatalf("retry finalization: %v", err)
	}
	if manager.active(session.id) != nil {
		t.Fatal("successfully finalized session remained registered")
	}
	if transcriptAttempts != 2 || statusAttempts != 1 || hookAttempts != 2 {
		t.Fatalf("attempts transcript=%d status=%d hook=%d", transcriptAttempts, statusAttempts, hookAttempts)
	}
}

func TestConsoleSessionManagerCloseAllWaitsForSessionClosedHook(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-hook-drain", "127.0.0.1", 22)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(),
			Done: testRuntimeDone(func() error { <-ctx.Done(); return ctx.Err() }), Close: func() error { return nil },
		}, nil
	}, nil)
	hookStarted := make(chan struct{})
	hookRelease := make(chan struct{})
	var startOnce sync.Once
	manager.SetSessionClosedHook(func(ctx context.Context, _ SessionHandle) error {
		startOnce.Do(func() { close(hookStarted) })
		select {
		case <-hookRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	record, err := manager.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	closeResult := make(chan error, 1)
	go func() { closeResult <- manager.CloseAll(ctx) }()
	select {
	case <-hookStarted:
	case <-time.After(time.Second):
		t.Fatal("session-closed hook did not start")
	}
	cancel()
	select {
	case err := <-closeResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CloseAll() error = %v, want canceled hook", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not return after cancellation")
	}
	if manager.active(record.ID) == nil {
		t.Fatal("session disappeared while its ownership hook was blocked")
	}
	close(hookRelease)
	if err := manager.CloseAll(t.Context()); err != nil {
		t.Fatalf("CloseAll() after hook release: %v", err)
	}
	if manager.active(record.ID) != nil {
		t.Fatal("session remained registered after hook completed")
	}
}

func TestConsoleSessionClosePathsRespectContextWhileTransportCloses(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(context.Context, *Manager, Record) error
	}{
		{name: "session", run: func(ctx context.Context, manager *Manager, record Record) error {
			return manager.Close(ctx, testExecutionPrincipal(), record.ID)
		}},
		{name: "runtime", run: func(ctx context.Context, manager *Manager, record Record) error {
			return manager.CloseRuntime(ctx, testExecutionPrincipal(), record.RuntimeID)
		}},
		{name: "recovery", run: func(ctx context.Context, manager *Manager, record Record) error {
			_, err := manager.RecoverRuntime(ctx, testExecutionPrincipal(), record.RuntimeID, nil)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			runtimeID := insertConsoleTestSSHProfile(t, database, "worker-bounded-"+test.name, "127.0.0.1", 22)
			release := make(chan struct{})
			var closeCalls atomic.Int32
			manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
				return &RuntimeSession{
					Stdin: &recordingWriteCloser{}, Output: testRuntimeOutput(),
					Done:  testRuntimeDone(func() error { <-release; return nil }),
					Close: func() error { closeCalls.Add(1); <-release; return nil },
				}, nil
			}, nil)
			record, err := manager.Create(t.Context(), CreateRequest{
				RuntimeID: runtimeID, Name: "active", Principal: testExecutionPrincipal(), WaitForStart: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer stop()
			if err := test.run(ctx, manager, record); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("close error = %v, want deadline exceeded", err)
			}
			close(release)
			if err := manager.CloseAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			if closeCalls.Load() != 1 {
				t.Fatalf("transport Close() calls = %d", closeCalls.Load())
			}
		})
	}
}

func TestConsoleSessionManagerEnsureReadyReturnsConnectionError(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 23)
	manager := NewManager(database, func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return nil, errors.New("transport dial: dial tcp 127.0.0.1:23: connect: connection refused")
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	handle, err := manager.EnsureReady(ctx, testExecutionPrincipal(), runtimeID)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected connection error, session=%d err=%v", handle.ID, err)
	}
	record, recordErr := manager.Get(context.Background(), handle.ID)
	if recordErr != nil {
		t.Fatalf("read failed session: %v", recordErr)
	}
	if record.Status != "error" || !strings.Contains(record.Error, "connection refused") {
		t.Fatalf("expected failed session record, got %#v", record)
	}
}

func TestConsoleSessionManagerReplaceIfCurrentUsesExactSessionCAS(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-cas", "127.0.0.1", 22)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin:  &recordingWriteCloser{},
			Output: testRuntimeOutput(),
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	principal := testExecutionPrincipal()
	first, err := manager.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Name: "first", Principal: principal, WaitForStart: true,
	})
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	manager.Resize(first.ID, 144, 41)

	if _, err := manager.ReplaceIfCurrent(context.Background(), principal, SessionHandle{}, CreateRequest{
		RuntimeID: runtimeID, Name: "unexpected concurrent session", Principal: principal, WaitForStart: true,
	}); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("replacement expecting no active session error = %v", err)
	}
	stale := SessionHandle{ID: first.ID - 1, RuntimeID: runtimeID, Generation: first.Generation}
	if _, err := manager.ReplaceIfCurrent(context.Background(), principal, stale, CreateRequest{
		RuntimeID: runtimeID, Name: "stale replacement", Principal: principal, WaitForStart: true,
	}); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("stale replacement error = %v", err)
	}
	active, err := manager.ActiveSnapshot(context.Background(), principal, runtimeID)
	if err != nil || active.ID != first.ID || active.Generation != first.Generation {
		t.Fatalf("stale replacement changed active session: %#v err=%v", active, err)
	}
	unrelated, err := manager.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Name: "unrelated", Principal: principal, WaitForStart: true,
	})
	if err != nil {
		t.Fatalf("create unrelated same-runtime session: %v", err)
	}

	type replaceResult struct {
		record Record
		err    error
	}
	start := make(chan struct{})
	results := make(chan replaceResult, 2)
	expected := SessionHandle{ID: first.ID, RuntimeID: runtimeID, Generation: first.Generation}
	for index := 0; index < 2; index++ {
		go func(index int) {
			<-start
			record, replaceErr := manager.ReplaceIfCurrent(context.Background(), principal, expected, CreateRequest{
				RuntimeID: runtimeID, Name: "concurrent-" + strconv.Itoa(index),
				Cols: 200, Rows: 60, Principal: principal, WaitForStart: true,
			})
			results <- replaceResult{record: record, err: replaceErr}
		}(index)
	}
	close(start)
	var winner Record
	successes := 0
	staleResults := 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.record
		case errors.Is(result.err, ErrSessionChanged):
			staleResults++
		default:
			t.Fatalf("concurrent replacement error = %v", result.err)
		}
	}
	if successes != 1 || staleResults != 1 {
		t.Fatalf("concurrent replacements successes=%d stale=%d", successes, staleResults)
	}
	if winner.ID == first.ID || winner.Generation <= first.Generation {
		t.Fatalf("replacement did not advance session identity: first=%#v winner=%#v", first, winner)
	}
	if winner.Cols != 144 || winner.Rows != 41 {
		t.Fatalf("replacement did not preserve current terminal geometry: %#v", winner)
	}
	active, err = manager.ActiveSnapshot(context.Background(), principal, runtimeID)
	if err != nil || active.ID != winner.ID || active.Generation != winner.Generation {
		t.Fatalf("replacement winner is not active: %#v err=%v", active, err)
	}
	unrelatedRecord, err := manager.Get(context.Background(), unrelated.ID)
	if err != nil || unrelatedRecord.Status != "connected" {
		t.Fatalf("exact replacement closed unrelated session: %#v err=%v", unrelatedRecord, err)
	}
	if err := manager.Close(context.Background(), principal, winner.ID); err != nil {
		t.Fatalf("close replacement: %v", err)
	}
	if err := manager.Close(context.Background(), principal, unrelated.ID); err != nil {
		t.Fatalf("close unrelated session: %v", err)
	}
}

func TestConsoleSessionManagerPreparesEnvironmentAfterPeerVerification(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-environment", "127.0.0.1", 22)
	events := []string{}
	manager := NewManager(database, func(ctx context.Context, request RuntimeOpenRequest) (*RuntimeSession, error) {
		if !request.HasEnvironment {
			t.Fatalf("runtime opener was not told to prepare environment transport")
		}
		events = append(events, "open")
		return &RuntimeSession{
			Stdin:        &recordingWriteCloser{},
			Output:       testRuntimeOutput(),
			PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(_ context.Context, environment *sessionenv.Envelope) error {
				events = append(events, "apply")
				return environment.WithEntries(func(entries []sessionenv.EntryView) error {
					if len(entries) != 1 || entries[0].Name != "API_TOKEN" ||
						string(entries[0].Value) != "secret-delivered-after-peer-check" {
						t.Fatalf("unexpected prepared environment: %#v", entries)
					}
					return nil
				})
			},
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	principal := testExecutionPrincipal()
	record, err := manager.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Name: "prepared", Principal: principal, WaitForStart: true,
		PrepareEnvironment: func(_ context.Context, peerIdentity string) (EnvironmentPreparation, error) {
			if peerIdentity != "SHA256:test-peer" {
				t.Fatalf("preparer peer identity = %q", peerIdentity)
			}
			events = append(events, "prepare")
			envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
				Name: "API_TOKEN", Value: []byte("secret-delivered-after-peer-check"),
			}})
			return EnvironmentPreparation{
				Environment: envelope,
				PostValidate: func(context.Context) error {
					events = append(events, "post-validate")
					return nil
				},
				Finalize: func(_ context.Context, handle SessionHandle) error {
					if handle.ID < 1 || handle.RuntimeID != runtimeID || handle.Generation < 1 {
						t.Fatalf("finalize handle = %#v", handle)
					}
					events = append(events, "finalize")
					return nil
				},
			}, err
		},
	})
	if err != nil {
		t.Fatalf("create prepared session: %v", err)
	}
	if got := strings.Join(events, ","); got != "open,prepare,apply,post-validate,finalize" {
		t.Fatalf("environment delivery order = %q", got)
	}
	if err := manager.Close(context.Background(), principal, record.ID); err != nil {
		t.Fatalf("close prepared session: %v", err)
	}
}

func TestConsoleSessionManagerFinalizationFailureNeverBecomesReady(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-finalization", "127.0.0.1", 22)
	closed := make(chan struct{}, 1)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin:        &recordingWriteCloser{},
			Output:       testRuntimeOutput(),
			PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error {
				return nil
			},
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error {
				select {
				case closed <- struct{}{}:
				default:
				}
				return nil
			},
		}, nil
	}, nil)
	_, err = manager.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Name: "finalization failure",
		Principal: testExecutionPrincipal(), WaitForStart: true,
		PrepareEnvironment: func(context.Context, string) (EnvironmentPreparation, error) {
			envelope, envelopeErr := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
				Name: "API_TOKEN", Value: []byte("secret-delivered-before-finalization"),
			}})
			return EnvironmentPreparation{
				Environment: envelope,
				Finalize: func(context.Context, SessionHandle) error {
					return errors.New("persist session binding failed")
				},
			}, envelopeErr
		},
	})
	if err == nil || !strings.Contains(err.Error(), "persist session binding failed") {
		t.Fatalf("finalization error = %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("runtime was not closed after finalization failure")
	}
	var status string
	if err := database.QueryRow(`
		SELECT status FROM console_sessions
		WHERE runtime_id = ? ORDER BY id DESC LIMIT 1`, runtimeID,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "connected" {
		t.Fatal("failed finalization session became ready")
	}
}

func TestConsoleSessionManagerPostDeliveryDriftNeverBecomesReady(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-drift", "127.0.0.1", 22)
	manager := NewManager(database, func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin:        &recordingWriteCloser{},
			Output:       testRuntimeOutput(),
			PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error {
				return nil
			},
			Done: testRuntimeDone(func() error {
				<-ctx.Done()
				return ctx.Err()
			}),
			Close: func() error { return nil },
		}, nil
	}, nil)
	_, err = manager.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Name: "drifted", Principal: testExecutionPrincipal(), WaitForStart: true,
		PrepareEnvironment: func(context.Context, string) (EnvironmentPreparation, error) {
			envelope, envelopeErr := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
				Name: "API_TOKEN", Value: []byte("secret-delivered-before-final-check"),
			}})
			return EnvironmentPreparation{
				Environment: envelope,
				PostValidate: func(context.Context) error {
					return errors.New("authorization changed during delivery")
				},
			}, envelopeErr
		},
	})
	if err == nil || !strings.Contains(err.Error(), "authorization changed during delivery") {
		t.Fatalf("post-delivery drift error = %v", err)
	}
	var status string
	if err := database.QueryRow(`
		SELECT status
		FROM console_sessions
		WHERE runtime_id = ?
		ORDER BY id DESC
		LIMIT 1`, runtimeID,
	).Scan(&status); err != nil {
		t.Fatalf("read drifted session: %v", err)
	}
	if status == "connected" {
		t.Fatalf("post-delivery drift session became attachable")
	}
}

func TestConsoleSessionManagerListGetAndCloseRuntime(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 'hello', 120, 32, ?, ?)`,
		runtimeID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := sessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read session id: %v", err)
	}

	manager := NewManager(database, nil, nil)
	items, err := manager.List(context.Background(), runtimeID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 || items[0].ID != sessionID || items[0].TargetName != "worker-1" || items[0].Transcript != "hello" {
		t.Fatalf("unexpected session list: %#v", items)
	}
	item, err := manager.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if item.ID != sessionID || item.Status != "connected" {
		t.Fatalf("unexpected session: %#v", item)
	}
	gotRuntimeID, err := manager.RuntimeID(context.Background(), sessionID)
	if err != nil || gotRuntimeID != runtimeID {
		t.Fatalf("session runtime id=%d, want %d: %v", gotRuntimeID, runtimeID, err)
	}
	if err := manager.CloseRuntime(context.Background(), testExecutionPrincipal(), runtimeID); err != nil {
		t.Fatalf("close server sessions: %v", err)
	}
	item, err = manager.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get closed session: %v", err)
	}
	if item.Status != "closed" || item.ClosedAt == nil {
		t.Fatalf("expected closed session, got %#v", item)
	}
}

func TestConsoleTranscriptPersistsAppendOnlyChunksAndBoundedSnapshot(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	runtimeID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 120, 32, ?, ?)`,
		runtimeID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := sessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read session id: %v", err)
	}

	manager := NewManager(database, nil, nil)
	output := strings.Repeat("a", maxConsoleSnapshotLength+10) + strings.Repeat("b", maxConsoleChunkLength+5)
	session := &managedConsoleSession{
		id:      sessionID,
		manager: manager,
		status:  "connected",
		clients: map[*websocket.Conn]*sync.Mutex{},
	}
	session.appendDisplayOutput(output)
	session.flushTranscript()

	var snapshot string
	if err := database.QueryRow(`SELECT transcript FROM console_sessions WHERE id = ?`, sessionID).Scan(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snapshot) != maxConsoleSnapshotLength {
		t.Fatalf("expected bounded snapshot length %d, got %d", maxConsoleSnapshotLength, len(snapshot))
	}
	if !strings.HasSuffix(snapshot, strings.Repeat("b", maxConsoleChunkLength+5)) {
		t.Fatalf("snapshot should keep the newest transcript tail")
	}

	var chunks int
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_session_chunks WHERE session_id = ?`, sessionID).Scan(&chunks); err != nil {
		t.Fatalf("count transcript chunks: %v", err)
	}
	if chunks < 2 {
		t.Fatalf("expected transcript to be split across chunks, got %d", chunks)
	}

	record, err := manager.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if record.Transcript != output {
		t.Fatalf("get should reconstruct transcript tail from chunks")
	}

	if _, err := database.Exec(`DELETE FROM console_sessions WHERE id = ?`, sessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_session_chunks WHERE session_id = ?`, sessionID).Scan(&chunks); err != nil {
		t.Fatalf("count chunks after cascade: %v", err)
	}
	if chunks != 0 {
		t.Fatalf("expected console transcript chunks to cascade delete, got %d", chunks)
	}
}

type manualHistoryRow struct {
	source         string
	command        string
	status         string
	trackingReason string
	stdout         string
	sessionID      sql.NullInt64
}
