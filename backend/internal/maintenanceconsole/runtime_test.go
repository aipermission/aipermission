package maintenanceconsole

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	if handled, status := RunMaintenanceConsoleSupervisorIfRequested(os.Args[1:]); handled {
		os.Exit(status)
	}
	os.Exit(m.Run())
}

func TestMaintenanceConsoleEnvironmentUsesExplicitAllowlist(t *testing.T) {
	environment := maintenanceConsoleEnvironment([]string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/app",
		"USER=aipermission",
		"LANG=C.UTF-8",
		"LC_TIME=tr_TR.UTF-8",
		"LC_PRIVATE_TOKEN=must-not-leak",
		"TERM=unsafe-parent-value",
		"AIPERMISSION_GATEWAY_SECRET=must-not-leak",
		"AIPERMISSION_ALLOWED_ORIGINS=http://localhost:3210",
		"HTTPS_PROXY=http://proxy.example.test",
		"GOOGLE_OAUTH_CLIENT_SECRET=must-not-leak",
	})

	for _, expected := range []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/app",
		"USER=aipermission",
		"LANG=C.UTF-8",
		"LC_TIME=tr_TR.UTF-8",
		"TERM=xterm-256color",
		"HISTFILE=/dev/null",
		"HISTSIZE=0",
		"HISTFILESIZE=0",
		"AIPERMISSION_MAINTENANCE_CONSOLE=1",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("maintenance environment should contain %q: %#v", expected, environment)
		}
	}

	for _, entry := range environment {
		if strings.Contains(entry, "must-not-leak") || strings.HasPrefix(entry, "AIPERMISSION_GATEWAY_SECRET=") ||
			strings.HasPrefix(entry, "AIPERMISSION_ALLOWED_ORIGINS=") || strings.HasPrefix(entry, "HTTPS_PROXY=") ||
			strings.HasPrefix(entry, "GOOGLE_OAUTH_CLIENT_SECRET=") || entry == "TERM=unsafe-parent-value" {
			t.Fatalf("maintenance environment inherited a denied value: %q", entry)
		}
	}
}

func TestMaintenanceConsoleBashDoesNotLoadUserStartupFiles(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash is not available")
	}
	home := t.TempDir()
	historyPath := filepath.Join(home, ".bash_history")
	if err := os.WriteFile(historyPath, []byte("__AIPERMISSION_OLD_HISTORY__\n"), 0o600); err != nil {
		t.Fatalf("write sentinel history: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("printf '__AIPERMISSION_BASHRC_LOADED__\\n'\n"), 0o600); err != nil {
		t.Fatalf("write sentinel bashrc: %v", err)
	}
	cmd, err := maintenanceConsoleCommand("/bin/bash", []string{"HOME=" + home, "PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatalf("create maintenance bash command: %v", err)
	}
	tty, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("start maintenance bash PTY: %v", err)
	}
	defer tty.Close()
	if _, err := io.WriteString(tty, "history; printf '__AIPERMISSION_READY__\\n'; exit\n"); err != nil {
		t.Fatalf("write maintenance bash command: %v", err)
	}
	_ = tty.SetReadDeadline(time.Now().Add(5 * time.Second))
	output, _ := io.ReadAll(tty)
	text := string(output)
	if strings.Contains(text, "__AIPERMISSION_BASHRC_LOADED__") {
		t.Fatalf("maintenance shell loaded user bashrc: %q", text)
	}
	if !strings.Contains(text, "__AIPERMISSION_READY__") {
		t.Fatalf("maintenance shell did not execute PTY input: %q", text)
	}
	if strings.Contains(text, "__AIPERMISSION_OLD_HISTORY__") {
		t.Fatalf("maintenance shell loaded persistent command history: %q", text)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("reap maintenance bash process: %v", err)
	}
	history, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read sentinel history: %v", err)
	}
	if string(history) != "__AIPERMISSION_OLD_HISTORY__\n" {
		t.Fatalf("maintenance shell changed persistent history: %q", history)
	}
}

func TestMaintenanceConsolePTYReadClosureTerminatesProcess(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		t.Fatalf("start maintenance console: %v", err)
	}
	pid := session.cmd.Process.Pid
	if err := session.writeInput("exec </dev/null >/dev/null 2>&1; exec sleep 20\n"); err != nil {
		t.Fatalf("write redirect command: %v", err)
	}
	waitForMaintenanceSignal(t, session.closed, "session close after PTY EOF")
	waitForMaintenanceSignal(t, session.processDone, "process reap after PTY EOF")
	if maintenanceConsoleProcessExists(pid) {
		t.Fatalf("maintenance process %d is still alive after PTY EOF", pid)
	}
}

func TestMaintenanceConsoleImmediateCloseDoesNotLeakSupervisor(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	for iteration := 0; iteration < 10; iteration++ {
		session, err := startMaintenanceConsoleSession()
		if err != nil {
			t.Fatalf("start maintenance console iteration %d: %v", iteration, err)
		}
		pid := session.cmd.Process.Pid
		session.Close()
		waitForMaintenanceSignal(t, session.processDone, "immediately closed supervisor reap")
		if maintenanceConsoleProcessExists(pid) {
			t.Fatalf("maintenance supervisor %d survived immediate close iteration %d", pid, iteration)
		}
	}
}

func TestMaintenanceConsoleCloseTerminatesSessionChildren(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		t.Fatalf("start maintenance console: %v", err)
	}
	t.Cleanup(session.Close)
	if err := session.writeInput("sh -c 'trap \"\" HUP; while :; do sleep 1; done' & echo __AIPERMISSION_CHILD_PID__:$!\n"); err != nil {
		t.Fatalf("start maintenance child: %v", err)
	}
	childPID := waitForMaintenanceChildPID(t, session)
	session.Close()
	waitForMaintenanceSignal(t, session.processDone, "maintenance process reap")
	if maintenanceConsoleProcessIsRunning(childPID) {
		t.Fatalf("maintenance child process %d survived session close", childPID)
	}
}

func TestMaintenanceConsoleCloseTerminatesChildThatCreatesANewSession(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not available")
	}
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		t.Fatalf("start maintenance console: %v", err)
	}
	t.Cleanup(session.Close)
	pidPath := filepath.Join(t.TempDir(), "detached.pid")
	if err := session.writeInput(fmt.Sprintf("env -i PATH=/usr/bin:/bin setsid sh -c 'echo $$ > %q; trap \"\" HUP TERM; while :; do sleep 1; done' &\n", pidPath)); err != nil {
		t.Fatalf("start detached maintenance child: %v", err)
	}
	childPID := waitForMaintenancePIDFile(t, pidPath)
	session.Close()
	if maintenanceConsoleProcessIsRunning(childPID) {
		t.Fatalf("detached maintenance child process %d survived session close", childPID)
	}
}

func TestMaintenanceConsoleSupervisorReapsExitedOrphanWhileSessionRemainsOpen(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		t.Fatalf("start maintenance console: %v", err)
	}
	t.Cleanup(session.Close)
	directory := t.TempDir()
	pidPath := filepath.Join(directory, "orphan.pid")
	scriptPath := filepath.Join(directory, "short-lived-child.sh")
	script := fmt.Sprintf("#!/bin/sh\necho $$ > %q\nsleep 0.1\n", pidPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write orphan helper: %v", err)
	}
	if err := session.writeInput(fmt.Sprintf("sh -c '%s &'\n", scriptPath)); err != nil {
		t.Fatalf("start short-lived orphan: %v", err)
	}
	childPID := waitForMaintenancePIDFile(t, pidPath)
	deadline := time.Now().Add(3 * time.Second)
	for maintenanceConsoleProcessExists(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if maintenanceConsoleProcessExists(childPID) {
		t.Fatalf("exited orphan process %d was not reaped while the console remained open", childPID)
	}
	if snapshot := session.Snapshot(); snapshot.Status != "connected" {
		t.Fatalf("reaping an orphan closed the maintenance session: %s", snapshot.Status)
	}
}

func TestMaintenanceConsoleRejectsClientRegistrationAfterClosingStarts(t *testing.T) {
	session := &Session{
		pty:     &os.File{},
		status:  "closing",
		clients: map[*websocket.Conn]*sync.Mutex{},
	}
	if _, ok := session.registerClient(nil, &sync.Mutex{}); ok {
		t.Fatal("closing maintenance session accepted a websocket client")
	}
}

func TestMaintenanceConsoleInitialFramesPrecedeConcurrentOutput(t *testing.T) {
	session := &Session{
		pty:        &os.File{},
		status:     "connected",
		shell:      "/bin/sh",
		transcript: "before\n",
		clients:    map[*websocket.Conn]*sync.Mutex{},
	}
	writeMu := &sync.Mutex{}
	registered := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	frames := []string{}
	var framesMu sync.Mutex
	done := make(chan bool, 1)
	go func() {
		done <- session.initializeClient(nil, writeMu, func(message maintenanceConsoleServerMessage) error {
			if message.Type == "snapshot" {
				close(registered)
				<-releaseSnapshot
			}
			framesMu.Lock()
			frames = append(frames, message.Type+":"+message.Data)
			framesMu.Unlock()
			return nil
		})
	}()
	<-registered
	if writeMu.TryLock() {
		writeMu.Unlock()
		t.Fatal("initial snapshot did not reserve the client writer")
	}
	outputStarted := make(chan struct{})
	outputDone := make(chan struct{})
	go func() {
		close(outputStarted)
		writeMu.Lock()
		framesMu.Lock()
		frames = append(frames, "output:after\n")
		framesMu.Unlock()
		writeMu.Unlock()
		close(outputDone)
	}()
	<-outputStarted
	close(releaseSnapshot)
	if ok := <-done; !ok {
		t.Fatal("client initialization failed")
	}
	<-outputDone
	framesMu.Lock()
	defer framesMu.Unlock()
	if got := strings.Join(frames, ","); got != "snapshot:before\n,ready:,output:after\n" {
		t.Fatalf("frame order = %q", got)
	}
}

func TestMaintenanceConsoleOutputBelongsToEitherSnapshotOrBroadcast(t *testing.T) {
	client := &websocket.Conn{}
	writeMu := &sync.Mutex{}

	appendedFirst := &Session{status: "connected", pty: &os.File{}, clients: map[*websocket.Conn]*sync.Mutex{}}
	if clients := appendedFirst.appendTranscriptAndClients("after\n"); len(clients) != 0 {
		t.Fatalf("output captured clients before registration: %d", len(clients))
	}
	snapshot, ok := appendedFirst.registerClient(client, writeMu)
	if !ok || snapshot.Transcript != "after\n" {
		t.Fatalf("post-output snapshot = %#v ok=%t", snapshot, ok)
	}

	registeredFirst := &Session{status: "connected", pty: &os.File{}, clients: map[*websocket.Conn]*sync.Mutex{}}
	snapshot, ok = registeredFirst.registerClient(client, writeMu)
	if !ok || snapshot.Transcript != "" {
		t.Fatalf("pre-output snapshot = %#v ok=%t", snapshot, ok)
	}
	clients := registeredFirst.appendTranscriptAndClients("after\n")
	if clients[client] != writeMu || len(clients) != 1 {
		t.Fatalf("registered client was not captured exactly once: %#v", clients)
	}
}

func TestMaintenanceConsoleInitialWriteFailureUnregistersClient(t *testing.T) {
	session := &Session{
		pty:     &os.File{},
		status:  "connected",
		clients: map[*websocket.Conn]*sync.Mutex{},
	}
	if session.initializeClient(nil, &sync.Mutex{}, func(maintenanceConsoleServerMessage) error {
		return io.ErrClosedPipe
	}) {
		t.Fatal("failed initial write reported a connected client")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.clients) != 0 {
		t.Fatalf("failed initial client remained registered: %d", len(session.clients))
	}
}

func TestMaintenanceConsoleClientCloseDoesNotWaitForBlockedWriter(t *testing.T) {
	requireMaintenanceConsoleSupported(t)
	session, err := startMaintenanceConsoleSession()
	if err != nil {
		t.Fatalf("start maintenance console: %v", err)
	}
	t.Cleanup(session.Close)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr == nil {
			session.Attach(ws)
		}
	}))
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("attach maintenance websocket: %v", err)
	}
	defer ws.Close()

	writeMu := waitForMaintenanceClientWriter(t, session)
	writeMu.Lock()
	done := make(chan struct{})
	go func() {
		session.closeClients()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		writeMu.Unlock()
		t.Fatal("maintenance client close waited for a blocked writer")
	}
	writeMu.Unlock()
}

func waitForMaintenanceSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func requireMaintenanceConsoleSupported(t *testing.T) {
	t.Helper()
	if !Supported() {
		t.Skip("maintenance console is not supported on this platform")
	}
}

func waitForMaintenanceChildPID(t *testing.T, session *Session) int {
	t.Helper()
	pattern := regexp.MustCompile(`__AIPERMISSION_CHILD_PID__:([0-9]+)`)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		match := pattern.FindStringSubmatch(session.Snapshot().Transcript)
		if len(match) == 2 {
			pid, err := strconv.Atoi(match[1])
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for maintenance child pid")
	return 0
}

func waitForMaintenancePIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for maintenance pid file %s", path)
	return 0
}

func waitForMaintenanceClientWriter(t *testing.T, session *Session) *sync.Mutex {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		for _, writeMu := range session.clients {
			session.mu.Unlock()
			return writeMu
		}
		session.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for maintenance websocket client")
	return nil
}
