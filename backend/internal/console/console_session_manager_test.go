package console

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
	"github.com/gorilla/websocket"
)

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
			Stdout: strings.NewReader(""),
			Wait: func() error {
				<-ctx.Done()
				return ctx.Err()
			},
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
			Stdout:       strings.NewReader(""),
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
			Wait: func() error {
				<-ctx.Done()
				return ctx.Err()
			},
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
			Stdout:       strings.NewReader(""),
			PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error {
				return nil
			},
			Wait: func() error {
				<-ctx.Done()
				return ctx.Err()
			},
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
			Stdout:       strings.NewReader(""),
			PeerIdentity: "SHA256:test-peer",
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error {
				return nil
			},
			Wait: func() error {
				<-ctx.Done()
				return ctx.Err()
			},
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
