package console

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

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

	result, err := session.execCommand(context.Background(), "docker ps", nil)
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

	result, err := session.execCommand(context.Background(), "docker ps", nil)
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
	result, err := session.execCommand(execCtx, "docker ps", nil)
	if !errors.Is(err, ErrCommandActive) {
		t.Fatalf("expected active command error, got %v", err)
	}
	if !result.Running || result.Command != "sleep 60" {
		t.Fatalf("expected active command metadata, got %#v", result)
	}
}

func TestManagedConsoleSessionRejectsManualInputBetweenAutomationFrames(t *testing.T) {
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	writes := make(chan string, 3)
	releasePrelude := make(chan struct{})
	session := &managedConsoleSession{
		id:     7,
		ctx:    sessionCtx,
		status: "connected",
		stdin:  &sequencedWriteCloser{writes: writes, releaseFirst: releasePrelude},
	}
	execCtx, cancelExec := context.WithCancel(context.Background())
	execDone := make(chan error, 1)
	go func() {
		_, err := session.execCommand(execCtx, "printf safe", nil)
		execDone <- err
	}()

	if first := <-writes; first != consoleExecPrelude() {
		t.Fatalf("first automation frame = %q", first)
	}
	manualDone := make(chan error, 1)
	go func() { manualDone <- session.submitManualInput("printf unsafe\n") }()
	select {
	case err := <-manualDone:
		t.Fatalf("manual input completed inside the automation prelude gap: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releasePrelude)
	if payload := <-writes; !strings.Contains(payload, "printf safe") {
		t.Fatalf("automation payload = %q", payload)
	}
	if err := <-manualDone; !errors.Is(err, ErrCommandActive) {
		t.Fatalf("manual input error = %v, want ErrCommandActive", err)
	}
	select {
	case extra := <-writes:
		t.Fatalf("manual input reached the terminal: %q", extra)
	case <-time.After(25 * time.Millisecond):
	}
	cancelExec()
	if err := <-execDone; err != nil {
		t.Fatalf("automation completion = %v", err)
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

	output := terminaltext.CleanDisplayOutput(input, false)
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

func TestAutomationCompletionSurvivesTranscriptTrimming(t *testing.T) {
	marker := "__AIPERMISSION_EXIT_TRIMMED__"
	session := &managedConsoleSession{
		status:        "connected",
		rawTranscript: strings.Repeat("x", maxConsoleTranscriptLength),
	}
	startOffset := session.rawStreamPositionLocked()
	session.activeExec = &consoleSessionActiveExec{Marker: marker, StartOffset: startOffset, Started: time.Now()}

	session.appendSafeOutput("\n" + strings.Repeat("y", maxConsoleTranscriptLength+17))
	session.appendSafeOutput("\nuseful output\n" + marker + ":42\n")

	output, exitCode, completed, err := session.checkCommandResult(startOffset, marker)
	if err != nil {
		t.Fatalf("check command result: %v", err)
	}
	if !completed || exitCode != 42 {
		t.Fatalf("expected completed exit 42, got completed=%v exit=%d", completed, exitCode)
	}
	if !strings.Contains(output, "useful output") {
		t.Fatalf("expected retained command output, got %q", output)
	}
}

func TestAutomationCompletionWaitsForEntireExitMarkerFrame(t *testing.T) {
	marker := "__AIPERMISSION_EXIT_FRAGMENTED__"
	frame := "\ncommand output\n" + marker + ":123\n"
	for split := 0; split < len(frame); split++ {
		t.Run(strconv.Itoa(split), func(t *testing.T) {
			session := &managedConsoleSession{status: "connected"}
			session.appendSafeOutput(frame[:split])
			_, _, completed, err := session.checkCommandResult(0, marker)
			if err != nil {
				t.Fatalf("partial frame returned error: %v", err)
			}
			if completed {
				t.Fatalf("partial frame completed at byte %d", split)
			}

			session.appendSafeOutput(frame[split:])
			output, exitCode, completed, err := session.checkCommandResult(0, marker)
			if err != nil {
				t.Fatalf("complete frame returned error: %v", err)
			}
			if !completed || exitCode != 123 || strings.TrimSpace(output) != "command output" {
				t.Fatalf("unexpected result completed=%v exit=%d output=%q", completed, exitCode, output)
			}
		})
	}
}

func TestAutomationTranscriptOffsetsPreserveUTF8Boundaries(t *testing.T) {
	session := &managedConsoleSession{status: "connected"}
	session.appendSafeOutput(strings.Repeat("a", maxConsoleTranscriptLength-1) + "€")
	if !utf8.ValidString(session.rawTranscript) {
		t.Fatalf("trimmed transcript is not valid UTF-8")
	}
	startOffset := session.rawStreamPositionLocked()
	marker := "__AIPERMISSION_EXIT_UTF8__"
	session.appendSafeOutput("\nçalıştı\n" + marker + ":0\n")
	output, exitCode, completed, err := session.checkCommandResult(startOffset, marker)
	if err != nil || !completed || exitCode != 0 || strings.TrimSpace(output) != "çalıştı" {
		t.Fatalf("unexpected UTF-8 result completed=%v exit=%d output=%q err=%v", completed, exitCode, output, err)
	}
}

func TestAutomationCompletionAcceptsMarkerAtTrimmedBufferBoundary(t *testing.T) {
	marker := "__AIPERMISSION_EXIT_BOUNDARY__"
	session := &managedConsoleSession{
		status:        "connected",
		rawTranscript: marker + ":0\n",
		rawBaseOffset: 25,
	}
	output, exitCode, completed, err := session.checkCommandResult(0, marker)
	if err != nil || !completed || exitCode != 0 || output != "" {
		t.Fatalf("unexpected boundary result completed=%v exit=%d output=%q err=%v", completed, exitCode, output, err)
	}
}

func TestAutomationCompletionRejectsMalformedTerminatedExitMarker(t *testing.T) {
	marker := "__AIPERMISSION_EXIT_MALFORMED__"
	session := &managedConsoleSession{status: "connected", rawTranscript: "\n" + marker + ":nope\n"}
	_, _, completed, err := session.checkCommandResult(0, marker)
	if err == nil || completed {
		t.Fatalf("malformed marker should fail without completing: completed=%v err=%v", completed, err)
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
	output := terminaltext.FormatAutomationCommand("set -e\n\ndocker ps\n")
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
	session.appendDisplayOutput(terminaltext.FormatAutomationCommand("pwd"))

	if !strings.Contains(session.transcript, "root@worker:~# \r\n[AI command]") {
		t.Fatalf("automation command should start after the current prompt line: %q", session.transcript)
	}
}
