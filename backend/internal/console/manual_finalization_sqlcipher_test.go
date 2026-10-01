package console

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
	"github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestConsoleSpontaneousCloseCompletesAdmittedLateManualHistory(t *testing.T) {
	for _, prompt := range []bool{false, true} {
		name := "without_prompt"
		if prompt {
			name = "with_prompt"
		}
		t.Run(name, func(t *testing.T) { testConsoleLateManualHistory(t, prompt) })
	}
}

func testConsoleLateManualHistory(t *testing.T, prompt bool) {
	database, manager, session, stopTransport, output := newConsoleLateHistoryFixture(t)
	database.SetMaxOpenConns(1)
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { _ = connection.Close() }) }
	inputDone, inputErrors := make(chan struct{}), make(chan error, 1)
	t.Cleanup(func() {
		release()
		stopTransport()
		session.cancel()
		waitConsoleHistoryWork(t, inputDone)
		waitConsoleHistoryWork(t, session.done)
	})
	waits := database.Stats().WaitCount
	go func() {
		defer close(inputDone)
		inputErrors <- manager.Input(t.Context(), session.principal, session.id, "echo $FIXTURE_VALUE\n")
	}()
	// A pool wait proves transport Write has returned and history persistence is
	// blocked. No transcript timer or other DB work has been started yet.
	waitConsoleHistoryCondition(t, func() bool { return database.Stats().WaitCount > waits })
	const canary = "late-history-fixture-secret"
	data := "echo $FIXTURE_VALUE\r\n" + canary + "\r\npartial-result\r\n"
	if prompt {
		data += "root@fixture:~# "
	}
	// Streaming exact redaction retains a suffix. Padding ensures the tested
	// prompt has reached the parser before transport finalization.
	output <- RuntimeOutput{Data: data + strings.Repeat(" ", len(canary)+32)}
	waitConsoleHistoryCondition(t, func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return strings.Contains(session.rawTranscript, "partial-result") && (!prompt || terminaltext.ManualTranscriptEndsWithPrompt(session.rawTranscript, "root@fixture:~# "))
	})
	stopTransport()
	waitConsoleHistoryAdmissionClosed(t, session)
	if _, status := sessionStatusAndCapture(session); status != "closed" {
		t.Fatalf("spontaneous finalization did not reach initial cleanup: %q", status)
	}
	release()
	select {
	case err := <-inputErrors:
		if err != nil {
			t.Fatalf("admitted input was rejected after dispatch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted input did not finish after database release")
	}
	waitConsoleHistoryWork(t, inputDone)
	waitConsoleHistoryWork(t, session.done)
	assertConsoleLateHistoryFinality(t, database, session, prompt, canary)
}

func newConsoleLateHistoryFixture(t *testing.T) (*sql.DB, *Manager, *managedConsoleSession, func(), chan RuntimeOutput) {
	t.Helper()
	database, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.ctx, session.cancel = context.WithCancel(t.Context())
	session.start, session.done = make(chan struct{}), make(chan struct{})
	session.rawTranscript = "root@fixture:~# "
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE_VALUE", Value: []byte("late-history-fixture-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	session.environment = environment
	t.Cleanup(environment.Destroy)
	output, transportDone := make(chan RuntimeOutput, 1), make(chan error, 1)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(output)
			transportDone <- nil
			close(transportDone)
		})
	}
	manager.openRuntime = func(context.Context, RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{
			Stdin: &cancellationWriter{}, Output: output, Done: transportDone,
			Close:            func() error { return nil },
			ApplyEnvironment: func(context.Context, *sessionenv.Envelope) error { return nil },
		}, nil
	}
	t.Cleanup(func() { stop(); session.cancel(); waitConsoleHistoryWork(t, session.done) })
	go session.run()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := session.waitStart(ctx); err != nil {
		t.Fatalf("start fixture runtime: %v", err)
	}
	return database, manager, session, stop, output
}

func assertConsoleLateHistoryFinality(t *testing.T, database *sql.DB, session *managedConsoleSession, prompt bool, canary string) {
	t.Helper()
	var status, projected, stdout, output, tracking, completed, projectedCompleted string
	if err := database.QueryRow(`
		SELECT cr.status, h.status, cr.stdout, h.output_text, cr.tracking_reason, cr.completed_at, h.completed_at
		FROM command_requests cr JOIN history_entries h ON h.source_ref_type = 'command_request' AND h.source_ref_id = cr.id
		WHERE cr.session_id = ? AND cr.source = 'manual'`, session.id).
		Scan(&status, &projected, &stdout, &output, &tracking, &completed, &projectedCompleted); err != nil {
		t.Fatalf("read final canonical and projected history: %v", err)
	}
	wantStatus, wantReason := "untracked", manualSessionClosed
	if prompt {
		wantStatus, wantReason = "completed", "exit_code_unavailable"
	}
	if status != wantStatus || projected != status || tracking != wantReason || completed == "" || projectedCompleted != completed {
		t.Fatalf("late history finality status=%q projected=%q reason=%q completed=%q projected_completed=%q", status, projected, tracking, completed, projectedCompleted)
	}
	for _, text := range []string{stdout, output} {
		if strings.Contains(text, canary) || !strings.Contains(text, "[REDACTED VAULT VALUE]") || !strings.Contains(text, "partial-result") {
			t.Fatalf("late persisted output lost exact redaction or content: %q", text)
		}
	}
	var sessionStatus, closedAt, transcript string
	if err := database.QueryRow(`SELECT status, closed_at, transcript FROM console_sessions WHERE id = ?`, session.id).Scan(&sessionStatus, &closedAt, &transcript); err != nil || sessionStatus != "closed" || closedAt == "" {
		t.Fatalf("session did not persist terminal state: %q %q %v", sessionStatus, closedAt, err)
	}
	if strings.Contains(transcript, canary) || !strings.Contains(transcript, "[REDACTED VAULT VALUE]") || !strings.Contains(transcript, "partial-result") {
		t.Fatalf("unsafe persisted session transcript: %q", transcript)
	}
	var leaks, masks int
	if err := database.QueryRow(`SELECT COALESCE(SUM(instr(data, ?)), 0), COALESCE(SUM(instr(data, '[REDACTED VAULT VALUE]')), 0)
		FROM console_session_chunks WHERE session_id = ?`, canary, session.id).Scan(&leaks, &masks); err != nil || leaks != 0 || masks == 0 {
		t.Fatalf("unsafe persisted session chunks: leaks=%d masks=%d err=%v", leaks, masks, err)
	}
	if active, _ := sessionStatusAndCapture(session); active != nil {
		t.Fatalf("finalization retained a manual capture: %#v", active)
	}
}

func TestConsoleFinalDrainClosesStaleManualRowsWithoutTouchingAutomatedRows(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)
	for _, source := range []string{"manual", "ai"} {
		result, err := database.Exec(`INSERT INTO command_requests
			(runtime_id, source, command, reason, status, session_id, created_at)
			VALUES (?, ?, 'fixture command', 'fixture', 'running', ?, ?)`, session.runtimeID, source, session.id, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if err := history.NewStore(database).SyncCommandRequest(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	session.finish("closed", "")
	session.destroySensitiveRuntime()
	for _, source := range []string{"manual", "ai"} {
		var status, projected string
		if err := database.QueryRow(`SELECT cr.status, h.status FROM command_requests cr
			JOIN history_entries h ON h.source_ref_type = 'command_request' AND h.source_ref_id = cr.id
			WHERE cr.session_id = ? AND cr.source = ?`, session.id, source).Scan(&status, &projected); err != nil {
			t.Fatal(err)
		}
		want := "running"
		if source == "manual" {
			want = "untracked"
		}
		if status != want || projected != want {
			t.Fatalf("wrong %s scope: canonical=%q projected=%q want=%q", source, status, projected, want)
		}
	}
}

func sessionStatusAndCapture(session *managedConsoleSession) (*consoleSessionManualCapture, string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.manualActive, session.status
}

func waitConsoleHistoryCondition(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("console history fixture did not reach its boundary")
		case <-ticker.C:
		}
	}
}
