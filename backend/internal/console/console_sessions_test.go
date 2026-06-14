package console

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/gorilla/websocket"
)

func TestManagedConsoleSessionCommandResultParsing(t *testing.T) {
	session := &managedConsoleSession{
		status:        "connected",
		rawTranscript: "prompt\ncommand output\n__AIPERMISSION_EXIT_1:42\nnext prompt",
	}

	output, exitCode, completed, err := session.checkCommandResult(0, "__AIPERMISSION_EXIT_1")
	if err != nil {
		t.Fatalf("check result: %v", err)
	}
	if !completed || exitCode != 42 || output != "prompt\ncommand output" {
		t.Fatalf("unexpected result completed=%v exit=%d output=%q", completed, exitCode, output)
	}
}

func TestManagedConsoleSessionReportsClosedBeforeCommandMarker(t *testing.T) {
	session := &managedConsoleSession{
		status:        "closed",
		rawTranscript: "partial output",
	}

	output, _, completed, err := session.checkCommandResult(0, "missing")
	if err == nil || completed || output != "partial output" {
		t.Fatalf("expected closed error with partial output, completed=%v output=%q err=%v", completed, output, err)
	}
}

func TestConsoleTranscriptLimitKeepsTail(t *testing.T) {
	value := strings.Repeat("a", maxConsoleTranscriptLength+20)
	limited := limitConsoleTranscript(value)
	if len(limited) != maxConsoleTranscriptLength {
		t.Fatalf("unexpected limited length: %d", len(limited))
	}
	if limited != value[20:] {
		t.Fatalf("transcript should keep the newest tail")
	}
}

func TestConsoleSessionManagerActiveForServerUsesNewestLiveSession(t *testing.T) {
	manager := &Manager{
		sessions: map[int64]*managedConsoleSession{
			1: {id: 1, runtimeProfileID: 10, status: "connected"},
			2: {id: 2, runtimeProfileID: 10, status: "closed"},
			3: {id: 3, runtimeProfileID: 10, status: "connected"},
			4: {id: 4, runtimeProfileID: 11, status: "connected"},
		},
	}

	selected := manager.activeForRuntimeProfile(10)
	if selected == nil || selected.id != 3 {
		t.Fatalf("expected newest live session 3, got %#v", selected)
	}
}

func TestConsoleSessionManagerEnforcesActiveSessionLimit(t *testing.T) {
	manager := &Manager{sessions: map[int64]*managedConsoleSession{}}
	for i := 0; i < maxActiveConsoleSessions; i++ {
		manager.sessions[int64(i+1)] = &managedConsoleSession{id: int64(i + 1), status: "connected"}
	}

	if _, err := manager.Create(context.Background(), CreateRequest{RuntimeProfileID: 1}); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("expected session limit error, got %v", err)
	}
}

func TestManagedConsoleSessionEnforcesClientLimit(t *testing.T) {
	session := &managedConsoleSession{clients: map[*websocket.Conn]*sync.Mutex{}}
	for i := 0; i < maxConsoleClientsPerSession; i++ {
		if _, err := session.addClient(&websocket.Conn{}); err != nil {
			t.Fatalf("add client %d: %v", i, err)
		}
	}
	if _, err := session.addClient(&websocket.Conn{}); !errors.Is(err, ErrClientLimit) {
		t.Fatalf("expected client limit error, got %v", err)
	}
}

func TestConsoleSessionInputLimit(t *testing.T) {
	manager := &Manager{sessions: map[int64]*managedConsoleSession{}}
	if err := manager.Input(context.Background(), 1, strings.Repeat("x", maxConsoleInputBytes+1)); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("expected input limit error, got %v", err)
	}
}

func TestManualInputCreatesUntrackedHistoryRow(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("nano /etc/hosts\r")

	row := readManualHistoryRow(t, database)
	if row.command != "nano /etc/hosts" || row.source != "manual" || row.status != "untracked" || row.trackingReason != "interactive_editor" || row.sessionID.Int64 != session.id {
		t.Fatalf("unexpected manual row: %#v", row)
	}
}

func TestManualInputHandlesBackspaceBeforeRecording(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("node --versio\x7fon\r")

	row := readManualHistoryRow(t, database)
	if row.command != "node --version" {
		t.Fatalf("expected backspace-adjusted command, got %q", row.command)
	}
}

func TestManualInputIgnoresHistoryRecallUntilEnter(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("\x1b[A")

	assertManualHistoryCount(t, database, 0)
}

func TestManualInputRecordsUnknownHistoryRecallOnEnter(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("\x1b[A\r")

	row := readManualHistoryRow(t, database)
	if row.command != "command recalled with arrow key" || row.status != "running" || row.trackingReason != "history_recall_untracked" {
		t.Fatalf("expected unknown history recall row, got %#v", row)
	}
}

func TestManualInputRecordsUntrustedPreviewWhenEscapeSequenceHasText(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("\x1b[A")
	session.recordManualInput(" --help\r")

	row := readManualHistoryRow(t, database)
	if row.command != "--help" || row.trackingReason != "untrusted_command_text" {
		t.Fatalf("expected untrusted preview row, got %#v", row)
	}
}

func TestManualInputRecordsBracketedPasteContent(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("\x1b[200~apt update\x1b[201~\r")

	row := readManualHistoryRow(t, database)
	if row.command != "apt update" || row.trackingReason != "manual_output_not_tracked" {
		t.Fatalf("expected bracketed paste command row, got %#v", row)
	}
}

func TestManualInputRecordsHeredocPreviewAndIgnoresBody(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("cat <<EOF\r\nsecret body\r\nEOF\r\n")

	assertManualHistoryCount(t, database, 1)
	row := readManualHistoryRow(t, database)
	if row.command != "cat <<EOF ..." || row.trackingReason != "multiline_or_heredoc" {
		t.Fatalf("unexpected heredoc row: %#v", row)
	}
}

func TestManualInputResumesAfterSplitHeredocTerminator(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("cat <<EOF\r")
	session.recordManualInput("secret body\r")
	session.recordManualInput("EOF\r")
	session.recordManualInput("pwd\r")

	assertManualHistoryCount(t, database, 2)
	row := readManualHistoryRow(t, database)
	if row.command != "pwd" || row.trackingReason != "manual_output_not_tracked" {
		t.Fatalf("expected command after heredoc terminator, got %#v", row)
	}
}

func TestManualInputResumesAfterCanceledHeredoc(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("cat <<EOF\r")
	session.recordManualInput("secret body\r")
	session.recordManualInput("\x03")
	session.recordManualInput("pwd\r")

	assertManualHistoryCount(t, database, 2)
	row := readManualHistoryRow(t, database)
	if row.command != "pwd" || row.trackingReason != "manual_output_not_tracked" {
		t.Fatalf("expected command after canceled heredoc, got %#v", row)
	}
}

func TestManualInputResumesAfterEndedHeredoc(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	session.recordManualInput("cat <<EOF\r")
	session.recordManualInput("secret body\r")
	session.recordManualInput("\x04")
	session.recordManualInput("pwd\r")

	assertManualHistoryCount(t, database, 2)
	row := readManualHistoryRow(t, database)
	if row.command != "pwd" || row.trackingReason != "manual_output_not_tracked" {
		t.Fatalf("expected command after ended heredoc, got %#v", row)
	}
}

func TestManualInputPausesWhileAutomationCommandIsActive(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)
	session.activeExec = &consoleSessionActiveExec{
		Command:     "apt update",
		Marker:      "__AIPERMISSION_EXIT_ACTIVE__",
		StartOffset: 0,
		Started:     time.Now(),
	}

	session.recordManualInput("ls\r")

	assertManualHistoryCount(t, database, 0)
}

func TestManualInputRedactsPersistedCommand(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	manager.redact = func(value string) string {
		return strings.ReplaceAll(value, "secret-token", "[REDACTED]")
	}

	session.recordManualInput("curl -H 'Authorization: Bearer secret-token' http://example.test\r")

	row := readManualHistoryRow(t, database)
	if strings.Contains(row.command, "secret-token") || !strings.Contains(row.command, "[REDACTED]") {
		t.Fatalf("expected redacted command, got %q", row.command)
	}
}

func TestConsoleManagerInputWritesPTYAndRecordsManualHistory(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "uptime\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	if stdin.String() != "uptime\n" {
		t.Fatalf("expected PTY input to be written unchanged, got %q", stdin.String())
	}
	row := readManualHistoryRow(t, database)
	if row.command != "uptime" || row.source != "manual" || row.status != "running" {
		t.Fatalf("unexpected manual history row: %#v", row)
	}
}

func TestManualInputCapturesOutputWhenPromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "node --version\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("node --version\r\nv24.1.0\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "node --version" || row.stdout != "v24.1.0" || row.trackingReason != "exit_code_unavailable" {
		t.Fatalf("expected captured manual output, got %#v", row)
	}
}

func TestManualInputCapturesOutputWhenBracketPromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.rawTranscript = "[~] # "
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "ls\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("ls\r\nindex_default.html\r\n[~] # ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "ls" || row.stdout != "index_default.html" || row.trackingReason != "exit_code_unavailable" {
		t.Fatalf("expected captured NAS prompt output, got %#v", row)
	}
}

func TestManualInputCapturesAptUpdateOutput(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "apt update\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("apt update\r\nHit:1 https://download.docker.com/linux/ubuntu noble InRelease\r\nReading package lists... Done\r\n9 packages can be upgraded. Run 'apt list --upgradable' to see them.\r\nroot@candle-query-1:~# ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "apt update" || row.status != "completed" || !strings.Contains(row.stdout, "9 packages can be upgraded") {
		t.Fatalf("expected apt update to be captured as completed, got %#v", row)
	}
}

func TestManualInputCapturesAptProgressOutput(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "apt update\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("apt update\r\nHit:1 https://download.docker.com/linux/ubuntu noble InRelease\r\n")
	session.appendOutput("Reading package lists... 85%\rReading package lists... 99%\rReading package lists... Done\r\n")
	session.appendOutput("Building dependency tree... 50%\rBuilding dependency tree... Done\r\n")
	session.appendOutput("Reading state information... Done\r\n9 packages can be upgraded. Run 'apt list --upgradable' to see them.\r\nroot@candle-query-1:~# ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "apt update" || row.status != "completed" || !strings.Contains(row.stdout, "Reading package lists") || !strings.Contains(row.stdout, "9 packages can be upgraded") {
		t.Fatalf("expected apt progress output to be captured as completed, got %#v", row)
	}
}

func TestManualInputDoesNotInferHistoryRecallFromShellEcho(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "\x1b[A"); err != nil {
		t.Fatalf("history recall input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "\r"); err != nil {
		t.Fatalf("enter input: %v", err)
	}
	session.appendOutput("apt update\r\nHit:1 https://download.docker.com/linux/ubuntu noble InRelease\r\n9 packages can be upgraded. Run 'apt list --upgradable' to see them.\r\nroot@candle-query-1:~# ")
	waitForManualHistoryStatus(t, database, "completed")
	assertManualHistoryCount(t, database, 1)
	row := readManualHistoryRow(t, database)
	if row.command != "command recalled with arrow key" || row.status != "completed" || row.trackingReason != "history_recall_untracked" || !strings.Contains(row.stdout, "9 packages can be upgraded") {
		t.Fatalf("history recall should capture output without inferring command text, got %#v", row)
	}
}

func TestManualInputPausesUnknownHistoryRecallWhenMoreInputArrives(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.rawTranscript = "root@worker:~# "
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "\x1b[A"); err != nil {
		t.Fatalf("history recall input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "\r"); err != nil {
		t.Fatalf("enter input: %v", err)
	}
	session.appendOutput("root@worker:~# docker exec -it f6f sh\r\n/ # ")
	if err := manager.Input(context.Background(), session.id, "test\n"); err != nil {
		t.Fatalf("nested input: %v", err)
	}
	waitForManualHistoryStatus(t, database, "untracked")
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("test\r\n/ # ")
	if err := manager.Input(context.Background(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), session.id, "pwd\n"); err != nil {
		t.Fatalf("host input after nested recall: %v", err)
	}
	session.appendOutput("pwd\r\n/home/root\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 2 {
		t.Fatalf("expected recalled row plus host row, got %#v", rows)
	}
	if rows[1].command != "command recalled with arrow key" || rows[1].status != "untracked" || rows[1].trackingReason != "history_recall_untracked" {
		t.Fatalf("expected unknown recall to become one untracked row, got %#v", rows[1])
	}
	if strings.Contains(rows[1].command, "test") || strings.Contains(rows[1].command, "exit") || strings.TrimSpace(rows[1].stdout) != "" {
		t.Fatalf("unknown recall should not append nested input or nested output, got %#v", rows[1])
	}
	if rows[0].command != "pwd" || rows[0].status != "completed" || rows[0].stdout != "/home/root" {
		t.Fatalf("expected host command after nested recall, got %#v", rows[0])
	}
}

func TestManualInputClearsStaleRunningRowsWhenPromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	insertStaleManualRunningRow(t, database, session, "old sleep")
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "node --version\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("node --version\r\nv24.1.0\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	if count := countManualRunningRows(t, database, session.id); count != 0 {
		t.Fatalf("expected stale manual running rows to be closed, got %d", count)
	}
}

func TestManualInputClearsStaleRunningRowsWhenCanceled(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	insertStaleManualRunningRow(t, database, session, "old apt update")
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "sleep 10\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("sleep 10\r\n^C\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "canceled")

	if count := countManualRunningRows(t, database, session.id); count != 0 {
		t.Fatalf("expected canceled manual command to clear stale running rows, got %d", count)
	}
}

func TestManualInputCompletesPreviousCommandBeforeRecordingNextInput(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "pwd\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	session.mu.Lock()
	session.rawTranscript = "pwd\r\n/home/root\r\nroot@worker:~# "
	session.mu.Unlock()

	if err := manager.Input(context.Background(), session.id, "hostname\n"); err != nil {
		t.Fatalf("second input: %v", err)
	}
	session.appendOutput("hostname\r\nworker-1\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 2 {
		t.Fatalf("expected two manual rows, got %#v", rows)
	}
	if rows[1].command != "pwd" || rows[1].stdout != "/home/root" || rows[1].status != "completed" {
		t.Fatalf("expected first row to keep output, got %#v", rows[1])
	}
	if rows[0].command != "hostname" || rows[0].stdout != "worker-1" || rows[0].status != "completed" {
		t.Fatalf("expected second row to complete, got %#v", rows[0])
	}
}

func TestManualInputAppendsNextLineAfterOutputStartsBeforePromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "sleep 1\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	session.appendOutput("sleep 1\r\npartial output\r\n")
	if err := manager.Input(context.Background(), session.id, "pwd\n"); err != nil {
		t.Fatalf("second input: %v", err)
	}
	session.appendOutput("pwd\r\n/home/root\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("expected one manual batch row, got %#v", rows)
	}
	if rows[0].command != "sleep 1\npwd" || rows[0].status != "completed" || !strings.Contains(rows[0].stdout, "partial output") || !strings.Contains(rows[0].stdout, "/home/root") {
		t.Fatalf("expected queued input to stay in one completed row, got %#v", rows[0])
	}
}

func TestManualInputKeepsSplitHealthCheckPasteAsSingleRow(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	firstChunk := strings.Join([]string{
		"set -e",
		`echo "== system =="`,
		"hostname",
		"uname -a",
		`echo "== disk =="`,
		"df -h | head -20",
		`echo "== memory =="`,
		"free -h",
		`echo "== docker containers =="`,
		`docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' 2>&1 || true`,
		`echo "== recent service errors =="`,
		"",
	}, "\n")
	secondChunk := `journalctl -p warning..alert --since "30 min ago" --no-pager 2>&1 | tail -80 || true` + "\n"

	if err := manager.Input(context.Background(), session.id, firstChunk); err != nil {
		t.Fatalf("first chunk input: %v", err)
	}
	session.appendOutput("set -e\r\necho \"== system ==\"\r\n== system ==\r\nworker-1\r\n")
	if err := manager.Input(context.Background(), session.id, secondChunk); err != nil {
		t.Fatalf("second chunk input: %v", err)
	}
	session.appendOutput("journalctl -p warning..alert --since \"30 min ago\" --no-pager 2>&1 | tail -80 || true\r\nJun 05 warning\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("expected one manual row for split health check paste, got %#v", rows)
	}
	if !strings.Contains(rows[0].command, "set -e") || !strings.Contains(rows[0].command, "journalctl -p warning..alert") {
		t.Fatalf("expected combined health check command, got %#v", rows[0])
	}
	if rows[0].status != "completed" || !strings.Contains(rows[0].stdout, "== system ==") || !strings.Contains(rows[0].stdout, "Jun 05 warning") {
		t.Fatalf("expected combined health check output, got %#v", rows[0])
	}
}

func TestManualInputPausesInsideNestedShellUntilOriginalPromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.rawTranscript = "root@worker:~# "
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "docker exec -it f6f sh\n"); err != nil {
		t.Fatalf("nested shell input: %v", err)
	}
	row := readManualHistoryRow(t, database)
	if row.command != "docker exec -it f6f sh" || row.status != "untracked" || row.trackingReason != "nested_shell" {
		t.Fatalf("expected nested shell row, got %#v", row)
	}

	session.appendOutput("root@worker:~# docker exec -it f6f sh\r\n/ # ")
	if err := manager.Input(context.Background(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), session.id, "pwd\n"); err != nil {
		t.Fatalf("host input after nested shell: %v", err)
	}
	session.appendOutput("pwd\r\n/home/root\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 2 {
		t.Fatalf("expected nested shell row plus later host row, got %#v", rows)
	}
	if rows[0].command != "pwd" || rows[0].status != "completed" || rows[0].stdout != "/home/root" {
		t.Fatalf("expected host command after nested shell, got %#v", rows[0])
	}
	if rows[1].command != "docker exec -it f6f sh" || rows[1].trackingReason != "nested_shell" {
		t.Fatalf("expected original nested shell row to remain, got %#v", rows[1])
	}
}

func TestManualInputRecordsSplitNestedShellCommandAsOneRow(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.rawTranscript = "root@worker:~# "
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "docker exec -it f6f "); err != nil {
		t.Fatalf("nested shell prefix input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "sh\n"); err != nil {
		t.Fatalf("nested shell suffix input: %v", err)
	}

	row := readManualHistoryRow(t, database)
	if row.command != "docker exec -it f6f sh" || row.status != "untracked" || row.trackingReason != "nested_shell" {
		t.Fatalf("expected split nested shell row, got %#v", row)
	}
}

func TestManualInputPausesNestedShellEvenWithoutKnownResumePrompt(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "docker exec -it f6f sh\n"); err != nil {
		t.Fatalf("nested shell input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("docker exec -it f6f sh\r\n/ # exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), session.id, "pwd\n"); err != nil {
		t.Fatalf("host input after nested shell: %v", err)
	}
	session.appendOutput("pwd\r\n/home/root\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 2 || rows[0].command != "pwd" || rows[1].command != "docker exec -it f6f sh" {
		t.Fatalf("expected nested row plus resumed host row, got %#v", rows)
	}
}

func TestManualPromptPrefixUsesCommandEchoLineForResumePrompt(t *testing.T) {
	transcript := "root@worker:~# docker exec -it f6f sh"
	if prompt := lastManualShellPrompt(transcript); prompt != "root@worker:~#" {
		t.Fatalf("expected prompt prefix from echo line, got %q", prompt)
	}
	if manualTranscriptEndsWithPrompt(transcript, "root@worker:~#") {
		t.Fatalf("command echo line must not count as returned prompt")
	}
}

func TestManualPromptPrefixSupportsBracketPathPrompts(t *testing.T) {
	transcript := "[/] # ls"
	if prompt := lastManualShellPrompt(transcript); prompt != "[/] #" {
		t.Fatalf("expected bracket prompt prefix from echo line, got %q", prompt)
	}
	if manualTranscriptEndsWithPrompt(transcript, "[/] #") {
		t.Fatalf("command echo line must not count as returned bracket prompt")
	}
	if !manualTranscriptEndsWithPrompt("[~] # ", "[~] #") {
		t.Fatalf("bare bracket prompt should count as returned prompt")
	}
}

func TestManualInputCloseSessionFinalizesRunningCapture(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "sleep 60\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.finish("closed", "")

	row := readManualHistoryRow(t, database)
	if row.status != "untracked" || row.trackingReason != manualSessionClosed {
		t.Fatalf("expected session close to finalize manual capture, got %#v", row)
	}
	if count := countManualRunningRows(t, database, session.id); count != 0 {
		t.Fatalf("expected no running manual rows after session close, got %d", count)
	}
}

func TestManualInputAutomationFinalizesRunningCapture(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.ctx = context.Background()
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "sleep 60\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, _ = session.execCommand(ctx, "echo ai")

	row := readManualHistoryRow(t, database)
	if row.command != "sleep 60" || row.status != "untracked" || row.trackingReason != manualActiveExecPaused {
		t.Fatalf("expected automation to pause manual capture, got %#v", row)
	}
}

func TestManualCapturedOutputDoesNotDropLinesEndingWithCommandText(t *testing.T) {
	output, _ := manualCapturedOutput("ls\r\ntools\r\nlogs\r\nroot@worker:~# ", "ls")
	if output != "tools\nlogs" {
		t.Fatalf("expected output lines ending with command text to survive, got %q", output)
	}
}

func TestManualInputCapturesMultilinePasteAsSingleRow(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	input := "echo hello\npwd\ntrue\n"
	if err := manager.Input(context.Background(), session.id, input); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("echo hello\r\nhello\r\npwd\r\n/home/root\r\ntrue\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("expected one manual row for multiline paste, got %#v", rows)
	}
	if rows[0].command != "echo hello\npwd\ntrue" || rows[0].stdout != "hello\n/home/root" {
		t.Fatalf("expected combined command and output, got %#v", rows[0])
	}
}

func TestManualInputCapturesSplitMultilinePasteAsSingleRow(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	for _, input := range []string{"echo hello\n", "pwd\n", "true\n"} {
		if err := manager.Input(context.Background(), session.id, input); err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
	}
	session.appendOutput("echo hello\r\nhello\r\npwd\r\n/home/root\r\ntrue\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("expected one manual row for split multiline paste, got %#v", rows)
	}
	if rows[0].command != "echo hello\npwd\ntrue" || rows[0].stdout != "hello\n/home/root" {
		t.Fatalf("expected combined command and output, got %#v", rows[0])
	}
}

func TestManualInputKeepsUnsafeMultilinePasteUntracked(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "nano test.txt\npwd\n"); err != nil {
		t.Fatalf("input: %v", err)
	}

	row := readManualHistoryRow(t, database)
	if row.command != "nano test.txt\npwd" || row.status != "untracked" || row.trackingReason != "compound_command" {
		t.Fatalf("expected unsafe multiline paste to be one untracked row, got %#v", row)
	}
}

func TestManualInputKeepsSplitUnsafeMultilinePasteUntracked(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "echo hello\n"); err != nil {
		t.Fatalf("safe input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "nano test.txt\n"); err != nil {
		t.Fatalf("unsafe input: %v", err)
	}

	waitForManualHistoryStatus(t, database, "untracked")
	row := readManualHistoryRow(t, database)
	if row.command != "echo hello\nnano test.txt" || row.trackingReason != "compound_command" {
		t.Fatalf("expected split unsafe paste to downgrade one row, got %#v", row)
	}
}

func TestManualInputCapturesOutputIfPromptReturnedBeforePersist(t *testing.T) {
	database, _, session := newManualHistoryTestSession(t)

	commands := session.prepareManualInput("pwd\n")
	session.appendOutput("pwd\r\n/home/root\r\nroot@worker:~# ")
	session.persistManualInput(commands)

	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "pwd" || row.stdout != "/home/root" {
		t.Fatalf("expected delayed persist to capture existing output, got %#v", row)
	}
}

func TestManualInputAppendsNextLineBeforePromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), session.id, "ls /root/nope\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	if err := manager.Input(context.Background(), session.id, "docker ps\n"); err != nil {
		t.Fatalf("second input: %v", err)
	}
	session.appendOutput("ls /root/nope\r\nls: cannot access '/root/nope': Permission denied\r\ndocker ps\r\nCONTAINER ID   IMAGE\r\nroot@worker:~# ")
	waitForManualHistoryStatus(t, database, "completed")

	rows := readManualHistoryRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("expected one manual row, got %#v", rows)
	}
	if rows[0].command != "ls /root/nope\ndocker ps" || rows[0].status != "completed" || !strings.Contains(rows[0].stdout, "CONTAINER ID") || !strings.Contains(rows[0].stdout, "Permission denied") {
		t.Fatalf("queued lines should complete as one batch, got %#v", rows[0])
	}
}

type recordingWriteCloser struct {
	strings.Builder
}

func (r *recordingWriteCloser) Close() error {
	return nil
}

func TestManagedConsoleSessionExecRejectsConcurrentAutomationCommand(t *testing.T) {
	session := &managedConsoleSession{
		id:            7,
		status:        "connected",
		rawTranscript: "prompt\nlong command output\n",
		activeExec: &consoleSessionActiveExec{
			Command:     "sleep 60",
			Marker:      "__AIPERMISSION_EXIT_ACTIVE__",
			StartOffset: 0,
			Started:     time.Now().Add(-time.Second),
		},
	}

	result, err := session.execCommand(context.Background(), "docker ps")
	if !errors.Is(err, ErrCommandActive) {
		t.Fatalf("expected active command error, got %v", err)
	}
	if !result.Running || result.Command != "sleep 60" || result.SessionID != 7 {
		t.Fatalf("expected active command metadata, got %#v", result)
	}
	if active := session.activeCommand(); active == nil || active.Command != "sleep 60" {
		t.Fatalf("concurrent exec must not replace active command: %#v", active)
	}
}

func TestManagedConsoleSessionExecRejectsCompletedActiveCommandUntilFinalized(t *testing.T) {
	session := &managedConsoleSession{
		id:            7,
		status:        "connected",
		rawTranscript: "prompt\nold output\n__AIPERMISSION_EXIT_ACTIVE__:0\n",
		activeExec: &consoleSessionActiveExec{
			Command:     "apt update",
			Marker:      "__AIPERMISSION_EXIT_ACTIVE__",
			StartOffset: 0,
			Started:     time.Now().Add(-time.Second),
		},
	}

	result, err := session.execCommand(context.Background(), "docker ps")
	if !errors.Is(err, ErrCommandActive) {
		t.Fatalf("expected active command error, got %v", err)
	}
	if result.Command != "apt update" || result.ExitCode != 0 {
		t.Fatalf("expected previous command metadata, got %#v", result)
	}
	if active := session.activeCommand(); active == nil || active.Command != "apt update" {
		t.Fatalf("completed active command should remain for background finalizer: %#v", active)
	}
}

func TestManagedConsoleSessionWaitActiveDoesNotBlockConcurrentExec(t *testing.T) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	session := &managedConsoleSession{
		id:            7,
		ctx:           sessionCtx,
		status:        "connected",
		rawTranscript: "prompt\nlong command output\n",
		activeExec: &consoleSessionActiveExec{
			Command:     "sleep 60",
			Marker:      "__AIPERMISSION_EXIT_ACTIVE__",
			StartOffset: 0,
			Started:     time.Now().Add(-time.Second),
		},
	}

	waitCtx, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	waitStarted := make(chan struct{})
	var once sync.Once
	go func() {
		once.Do(func() { close(waitStarted) })
		_, _ = session.waitActiveCommand(waitCtx)
	}()
	<-waitStarted
	time.Sleep(25 * time.Millisecond)

	execCtx, cancelExec := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelExec()
	result, err := session.execCommand(execCtx, "docker ps")
	if !errors.Is(err, ErrCommandActive) {
		t.Fatalf("expected active command error, got %v", err)
	}
	if !result.Running || result.Command != "sleep 60" {
		t.Fatalf("expected active command metadata, got %#v", result)
	}
}

func TestConsoleExecPayloadAvoidsBase64BashAndMktemp(t *testing.T) {
	payload := consoleExecPayload("printf 'hello\\n'\n", "__AIPERMISSION_EXIT_TEST__")
	for _, forbidden := range []string{"base64", "mktemp", "bash "} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("payload should not depend on %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(payload, "/bin/sh <<'__AIPERMISSION_EXIT_TEST___SCRIPT'") {
		t.Fatalf("payload should run command through a quoted /bin/sh heredoc: %s", payload)
	}
	if !strings.Contains(payload, "__AIPERMISSION_EXIT_TEST__:%s") {
		t.Fatalf("payload should emit the exit marker: %s", payload)
	}
	if !strings.Contains(payload, ") </dev/null") {
		t.Fatalf("payload should keep command stdin from consuming the heredoc: %s", payload)
	}
	if !strings.Contains(payload, "stty \"$__aipermission_saved_stty\" 2>/dev/null || true") {
		t.Fatalf("payload should restore the original terminal input mode when possible: %s", payload)
	}
	if !strings.Contains(payload, "stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true") {
		t.Fatalf("payload should restore terminal input mode before completion marker: %s", payload)
	}
}

func TestConsoleExecPreludeDisablesEchoBeforePayload(t *testing.T) {
	prelude := consoleExecPrelude()
	if !strings.Contains(prelude, "__aipermission_saved_stty=$(stty -g 2>/dev/null || true)") {
		t.Fatalf("prelude should save terminal input mode before disabling echo: %s", prelude)
	}
	if !strings.Contains(prelude, "stty -echo 2>/dev/null || true") {
		t.Fatalf("prelude should disable terminal echo before command payload is written: %s", prelude)
	}
}

func TestConsoleExecPayloadRunsAndEmitsMarker(t *testing.T) {
	payload := consoleExecPayload("set -e\nprintf 'hello\\n'\nprintf 'world\\n'\n", "__AIPERMISSION_EXIT_TEST__")
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader(payload)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("payload shell returned error: %v output=%s", err, outputBytes)
	}
	output := string(outputBytes)
	if !strings.Contains(output, "hello\nworld") {
		t.Fatalf("payload should run command body, got %q", output)
	}
	if !strings.Contains(output, "__AIPERMISSION_EXIT_TEST__:0") {
		t.Fatalf("payload should print success marker, got %q", output)
	}
}

func TestConsoleExecPayloadPreservesFailureMarkerWithSetE(t *testing.T) {
	payload := consoleExecPayload("set -e\nprintf 'before-fail\\n'\nfalse\nprintf 'after-fail\\n'\n", "__AIPERMISSION_EXIT_TEST__")
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader(payload)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outer payload shell should restore and exit cleanly: %v output=%s", err, outputBytes)
	}
	output := string(outputBytes)
	if !strings.Contains(output, "before-fail") || strings.Contains(output, "after-fail") {
		t.Fatalf("payload should run failing set -e command without continuing inside user body, got %q", output)
	}
	if !strings.Contains(output, "__AIPERMISSION_EXIT_TEST__:1") {
		t.Fatalf("payload should still print failure marker after set -e command body, got %q", output)
	}
}

func TestConsoleExecPayloadDoesNotLetCommandConsumeMarkerScript(t *testing.T) {
	payload := consoleExecPayload("cat\nprintf 'after-cat\\n'\n", "__AIPERMISSION_EXIT_TEST__")
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader(payload)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("payload shell returned error: %v output=%s", err, outputBytes)
	}
	output := string(outputBytes)
	if !strings.Contains(output, "after-cat") {
		t.Fatalf("payload should continue after stdin-reading command, got %q", output)
	}
	if !strings.Contains(output, "__AIPERMISSION_EXIT_TEST__:0") {
		t.Fatalf("payload should still print marker after stdin-reading command, got %q", output)
	}
}

func TestCleanConsoleDisplayOutputRemovesInternalExecNoise(t *testing.T) {
	input := "root@worker:~# __aipermission_saved_ps2=${PS2-}\r\n" +
		"root@worker:~# PS2=\r\n" +
		"root@worker:~# stty -echo\r\n" +
		"root@worker:~# stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true\r\n" +
		"\r\n\r\n" +
		"--- images before ---\r\n" +
		"nginx alpine\r\n" +
		"\r\n" +
		"__AIPERMISSION_EXIT_1_2__:0\r\n" +
		"root@worker:~# \r\n" +
		"\r\n\r\n" +
		"--- after prompt ---\r\n"

	output := cleanConsoleDisplayOutput(input, false)
	for _, forbidden := range []string{"root@worker", "__aipermission", "PS2=", "stty -echo", "stty sane", "__AIPERMISSION_EXIT", "\r\n\r\n"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("display output should remove %q noise: %q", forbidden, output)
		}
	}
	if !strings.Contains(output, "--- images before ---") || !strings.Contains(output, "nginx alpine") || !strings.Contains(output, "--- after prompt ---") {
		t.Fatalf("display output should keep useful lines: %q", output)
	}
}

func TestAppendOutputKeepsManualPTYCharacters(t *testing.T) {
	session := &managedConsoleSession{}
	session.appendOutput("cd /opt/aiperm demo")
	session.appendOutput("\b\b")
	session.appendOutput("data")

	if session.transcript != "cd /opt/aiperm demo\b\bdata" {
		t.Fatalf("manual output should stay raw, got %q", session.transcript)
	}
}

func TestAppendOutputCleansOnlyDuringAutomation(t *testing.T) {
	session := &managedConsoleSession{
		activeExec: &consoleSessionActiveExec{Marker: "__AIPERMISSION_EXIT_1"},
	}
	session.appendOutput("root@worker:~# PS2=\r\n\r\nuseful\r\n__AIPERMISSION_EXIT_1:0\r\n")

	if strings.Contains(session.transcript, "PS2=") || strings.Contains(session.transcript, "__AIPERMISSION_EXIT") || strings.Contains(session.transcript, "\r\n\r\n") {
		t.Fatalf("automation output should be cleaned for display: %q", session.transcript)
	}
	if !strings.Contains(session.rawTranscript, "__AIPERMISSION_EXIT_1:0") {
		t.Fatalf("raw transcript should keep marker for parsing: %q", session.rawTranscript)
	}
	if !strings.Contains(session.transcript, "useful") {
		t.Fatalf("display transcript should keep useful output: %q", session.transcript)
	}
}

func TestAppendOutputHidesConsolePreludeNoise(t *testing.T) {
	session := &managedConsoleSession{
		activeExec: &consoleSessionActiveExec{Marker: "__AIPERMISSION_EXIT_1"},
	}
	session.appendOutput("__aipermission_saved_stty=$(stty -g 2>/dev/null || true)\r\nuseful output\r\n__AIPERMISSION_EXIT_1:0\r\n")

	if strings.Contains(session.transcript, "__aipermission_saved_stty") {
		t.Fatalf("display transcript should hide console prelude noise: %q", session.transcript)
	}
	if !strings.Contains(session.transcript, "useful output") {
		t.Fatalf("display transcript should keep command output: %q", session.transcript)
	}
}

func TestCheckCommandResultFiltersConsolePreludeNoise(t *testing.T) {
	session := &managedConsoleSession{
		rawTranscript: "__aipermission_saved_stty=$(stty -g 2>/dev/null || true)\nuseful output\n__AIPERMISSION_EXIT_1:0\nroot@worker:~# ",
	}

	output, exitCode, completed, err := session.checkCommandResult(0, "__AIPERMISSION_EXIT_1")
	if err != nil {
		t.Fatalf("check command result: %v", err)
	}
	if !completed || exitCode != 0 {
		t.Fatalf("expected completed success, got completed=%v exit=%d", completed, exitCode)
	}
	if strings.Contains(output, "__aipermission_saved_stty") {
		t.Fatalf("command result should hide console prelude noise: %q", output)
	}
	if strings.TrimSpace(output) != "useful output" {
		t.Fatalf("unexpected command output: %q", output)
	}
}

func TestAppendOutputKeepsFinalPromptAfterAutomationCompletes(t *testing.T) {
	session := &managedConsoleSession{
		activeExec: &consoleSessionActiveExec{Marker: "__AIPERMISSION_EXIT_1"},
	}
	session.appendOutput("useful\r\n__AIPERMISSION_EXIT_1:0\r\n")
	session.clearActiveCommand("__AIPERMISSION_EXIT_1")
	session.appendOutput("root@worker:~# \r\n\r\n\r\n")

	if !strings.Contains(session.transcript, "root@worker:~#") {
		t.Fatalf("post-automation prompt should stay visible: %q", session.transcript)
	}
	if strings.Contains(session.transcript, "\r\n\r\n") {
		t.Fatalf("post-automation blank tail should be filtered: %q", session.transcript)
	}
	if !strings.Contains(session.rawTranscript, "root@worker:~#") {
		t.Fatalf("raw transcript should keep prompt tail for diagnostics: %q", session.rawTranscript)
	}
}

func TestAppendOutputKeepsPromptAfterMarkerWhileAutomationStillActive(t *testing.T) {
	session := &managedConsoleSession{
		activeExec: &consoleSessionActiveExec{Marker: "__AIPERMISSION_EXIT_1"},
	}
	session.appendOutput("useful\r\n__AIPERMISSION_EXIT_1:0\r\n")
	session.appendOutput("root@worker:~# ")

	if !strings.Contains(session.transcript, "root@worker:~#") {
		t.Fatalf("prompt after marker should stay visible even before active command is cleared: %q", session.transcript)
	}
	if strings.Contains(session.transcript, "__AIPERMISSION_EXIT") {
		t.Fatalf("display transcript should still hide internal marker: %q", session.transcript)
	}
}

func TestAppendOutputKeepsManualOutputAfterFilterWindow(t *testing.T) {
	session := &managedConsoleSession{
		filterUntil: time.Now().Add(-time.Second),
	}
	session.appendOutput("manual line\r\n\r\n")

	if session.transcript != "manual line\r\n\r\n" {
		t.Fatalf("manual output after filter window should stay raw: %q", session.transcript)
	}
}

func TestFormatAutomationCommandShowsCommandLines(t *testing.T) {
	output := formatAutomationCommand("set -e\n\ndocker ps\n")
	if !strings.Contains(output, "[AI command]") ||
		!strings.Contains(output, "$ set -e") ||
		!strings.Contains(output, "$ docker ps") {
		t.Fatalf("automation command should be visible in display transcript: %q", output)
	}
	if strings.Contains(output, "$ \r\n") || strings.HasPrefix(output, "\r\n") {
		t.Fatalf("automation command should avoid blank command rows and leading blank lines: %q", output)
	}
}

func TestAppendDisplayOutputSeparatesAutomationCommandFromPrompt(t *testing.T) {
	session := &managedConsoleSession{
		transcript: "root@worker:~# ",
	}
	session.appendDisplayOutput(formatAutomationCommand("pwd"))

	if !strings.Contains(session.transcript, "root@worker:~# \r\n[AI command]") {
		t.Fatalf("automation command should start after the current prompt line: %q", session.transcript)
	}
}

func TestConsoleSessionManagerCreateValidationAndCloseInactive(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	manager := NewManager(database, nil, nil)

	if _, err := manager.Create(context.Background(), CreateRequest{}); err == nil {
		t.Fatalf("expected missing server id to fail")
	}
	if err := manager.Close(context.Background(), 999); err != nil {
		t.Fatalf("closing inactive/missing session should be idempotent: %v", err)
	}
}

func TestConsoleSessionManagerEnsureReadyReturnsConnectionError(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeProfileID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 23)
	manager := NewManager(database, func(context.Context, int64, int, int) (*RuntimeSession, error) {
		return nil, errors.New("transport dial: dial tcp 127.0.0.1:23: connect: connection refused")
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sessionID, err := manager.EnsureReady(ctx, runtimeProfileID)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected connection error, session=%d err=%v", sessionID, err)
	}
	record, recordErr := manager.Get(context.Background(), sessionID)
	if recordErr != nil {
		t.Fatalf("read failed session: %v", recordErr)
	}
	if record.Status != "error" || !strings.Contains(record.Error, "connection refused") {
		t.Fatalf("expected failed session record, got %#v", record)
	}
}

func TestConsoleSessionManagerListGetAndCloseRuntimeProfile(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	runtimeProfileID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_profile_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 'hello', 120, 32, ?, ?)`,
		runtimeProfileID,
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
	items, err := manager.List(context.Background(), runtimeProfileID)
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
	if err := manager.CloseRuntimeProfile(context.Background(), runtimeProfileID); err != nil {
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
	runtimeProfileID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_profile_id, name, status, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 120, 32, ?, ?)`,
		runtimeProfileID,
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

func insertConsoleTestSSHProfile(t *testing.T, database *sql.DB, name string, host string, port int) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	targetResult, err := database.Exec(`
		INSERT INTO connector_targets (connector_kind, name, config_json, created_at, updated_at)
		VALUES ('ssh', ?, ?, ?, ?)`,
		name,
		`{"host":"`+host+`","port":`+strconv.Itoa(port)+`}`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("read target id: %v", err)
	}
	profileResult, err := database.Exec(`
		INSERT INTO connector_credential_profiles (target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at)
		VALUES (?, 'ssh', 'private_key', 'root', '{"username":"root","ssh_key_id":0}', '', ?, ?)`,
		targetID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := profileResult.LastInsertId()
	if err != nil {
		t.Fatalf("read profile id: %v", err)
	}
	return profileID
}

func newManualHistoryTestSession(t *testing.T) (*sql.DB, *Manager, *managedConsoleSession) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "console.db"), "ConsolePassword123")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	runtimeProfileID := insertConsoleTestSSHProfile(t, database, "worker-1", "127.0.0.1", 22)
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (runtime_profile_id, name, status, cols, rows, created_at, updated_at)
		VALUES (?, 'manual', 'connected', 120, 32, ?, ?)`,
		runtimeProfileID,
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
	session := &managedConsoleSession{
		id:               sessionID,
		runtimeProfileID: runtimeProfileID,
		manager:          manager,
		status:           "connected",
		clients:          map[*websocket.Conn]*sync.Mutex{},
	}
	return database, manager, session
}

func readManualHistoryRow(t *testing.T, database *sql.DB) manualHistoryRow {
	t.Helper()
	var row manualHistoryRow
	if err := database.QueryRow(`
		SELECT source, command, status, tracking_reason, stdout, session_id
		FROM command_requests
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID); err != nil {
		t.Fatalf("read manual history row: %v", err)
	}
	return row
}

func readManualHistoryRows(t *testing.T, database *sql.DB) []manualHistoryRow {
	t.Helper()
	rows, err := database.Query(`
		SELECT source, command, status, tracking_reason, stdout, session_id
		FROM command_requests
		WHERE source = 'manual'
		ORDER BY id DESC`)
	if err != nil {
		t.Fatalf("read manual history rows: %v", err)
	}
	defer rows.Close()

	items := []manualHistoryRow{}
	for rows.Next() {
		var row manualHistoryRow
		if err := rows.Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID); err != nil {
			t.Fatalf("scan manual history row: %v", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate manual history rows: %v", err)
	}
	return items
}

func waitForManualHistoryStatus(t *testing.T, database *sql.DB, status string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		var row manualHistoryRow
		err := database.QueryRow(`
			SELECT source, command, status, tracking_reason, stdout, session_id
			FROM command_requests
			WHERE source = 'manual'
			ORDER BY id DESC
			LIMIT 1`,
		).Scan(&row.source, &row.command, &row.status, &row.trackingReason, &row.stdout, &row.sessionID)
		if err == nil && row.status == status {
			return
		}
		if time.Now().After(deadline) {
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("read manual history status: %v", err)
			}
			t.Fatalf("manual history status did not become %q, latest row %#v", status, row)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertManualHistoryCount(t *testing.T, database *sql.DB, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM command_requests WHERE source = 'manual'`).Scan(&count); err != nil {
		t.Fatalf("count manual history rows: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d manual history rows, got %d", expected, count)
	}
}

func insertStaleManualRunningRow(t *testing.T, database *sql.DB, session *managedConsoleSession, command string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`
		INSERT INTO command_requests (runtime_profile_id, source, command, reason, status, tracking_reason, session_id, created_at)
		VALUES (?, 'manual', ?, 'manual console command', 'running', 'manual_output_not_tracked', ?, ?)`,
		session.runtimeProfileID,
		command,
		session.id,
		now,
	); err != nil {
		t.Fatalf("insert stale manual running row: %v", err)
	}
}

func countManualRunningRows(t *testing.T, database *sql.DB, sessionID int64) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM command_requests
		WHERE source = 'manual' AND status = 'running' AND session_id = ?`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count manual running rows: %v", err)
	}
	return count
}
