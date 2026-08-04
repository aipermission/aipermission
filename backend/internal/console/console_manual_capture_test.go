package console

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "uptime\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "node --version\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "ls\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("ls\r\nindex_default.html\r\n[~] # ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "ls" || row.stdout != "index_default.html" || row.trackingReason != "exit_code_unavailable" {
		t.Fatalf("expected captured NAS prompt output, got %#v", row)
	}
}

func TestManualInputCapturesOutputWhenPathPromptReturns(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	session.rawTranscript = "/ # "
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	session.appendOutput("pwd\r\n/\r\n/ # ")
	waitForManualHistoryStatus(t, database, "completed")
	row := readManualHistoryRow(t, database)
	if row.command != "pwd" || row.stdout != "/" || row.trackingReason != "exit_code_unavailable" {
		t.Fatalf("expected captured Kubernetes path prompt output, got %#v", row)
	}
}

func TestManualInputCapturesAptUpdateOutput(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "apt update\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "apt update\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "\x1b[A"); err != nil {
		t.Fatalf("history recall input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "\r"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "\x1b[A"); err != nil {
		t.Fatalf("history recall input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "\r"); err != nil {
		t.Fatalf("enter input: %v", err)
	}
	session.appendOutput("root@worker:~# docker exec -it f6f sh\r\n/ # ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "test\n"); err != nil {
		t.Fatalf("nested input: %v", err)
	}
	waitForManualHistoryStatus(t, database, "untracked")
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("test\r\n/ # ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "node --version\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "sleep 10\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	session.mu.Lock()
	session.rawTranscript = "pwd\r\n/home/root\r\nroot@worker:~# "
	session.mu.Unlock()

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "hostname\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "sleep 1\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	session.appendOutput("sleep 1\r\npartial output\r\n")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, firstChunk); err != nil {
		t.Fatalf("first chunk input: %v", err)
	}
	session.appendOutput("set -e\r\necho \"== system ==\"\r\n== system ==\r\nworker-1\r\n")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, secondChunk); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "docker exec -it f6f sh\n"); err != nil {
		t.Fatalf("nested shell input: %v", err)
	}
	row := readManualHistoryRow(t, database)
	if row.command != "docker exec -it f6f sh" || row.status != "untracked" || row.trackingReason != "nested_shell" {
		t.Fatalf("expected nested shell row, got %#v", row)
	}

	session.appendOutput("root@worker:~# docker exec -it f6f sh\r\n/ # ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "docker exec -it f6f "); err != nil {
		t.Fatalf("nested shell prefix input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "sh\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "docker exec -it f6f sh\n"); err != nil {
		t.Fatalf("nested shell input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "exit\n"); err != nil {
		t.Fatalf("nested exit input: %v", err)
	}
	assertManualHistoryCount(t, database, 1)

	session.appendOutput("docker exec -it f6f sh\r\n/ # exit\r\nroot@worker:~# ")
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "pwd\n"); err != nil {
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

func TestManualPromptPrefixSupportsKubernetesPathPrompts(t *testing.T) {
	transcript := "/ # ls"
	if prompt := lastManualShellPrompt(transcript); prompt != "/ #" {
		t.Fatalf("expected Kubernetes path prompt prefix from echo line, got %q", prompt)
	}
	if manualTranscriptEndsWithPrompt(transcript, "/ #") {
		t.Fatalf("command echo line must not count as returned path prompt")
	}
	if !manualTranscriptEndsWithPrompt("/app $ ", "/app $") {
		t.Fatalf("bare path prompt should count as returned prompt")
	}
}

func TestManualInputCloseSessionFinalizesRunningCapture(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	stdin := &recordingWriteCloser{}
	session.stdin = stdin
	manager.sessions[session.id] = session

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "sleep 60\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "sleep 60\n"); err != nil {
		t.Fatalf("input: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, _ = session.execCommand(ctx, "echo ai", nil)

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
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, input); err != nil {
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
		if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, input); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "nano test.txt\npwd\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "echo hello\n"); err != nil {
		t.Fatalf("safe input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "nano test.txt\n"); err != nil {
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

	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "ls /root/nope\n"); err != nil {
		t.Fatalf("first input: %v", err)
	}
	if err := manager.Input(context.Background(), testExecutionPrincipal(), session.id, "docker ps\n"); err != nil {
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

type blockingWriteCloser struct {
	started chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (w *blockingWriteCloser) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.proceed
	return len(data), nil
}

func (w *blockingWriteCloser) Close() error {
	return nil
}
