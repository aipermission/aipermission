package console

import (
	"strings"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
	"github.com/aipermission/aipermission/backend/internal/testkit/websocketpipe"
	"github.com/gorilla/websocket"
)

func TestConsoleConcurrentOutputKeepsTranscriptOrder(t *testing.T) {
	for _, laterPath := range []string{"manual", "display"} {
		t.Run(laterPath, func(t *testing.T) { testConsoleConcurrentOutputOrder(t, laterPath) })
	}
}

func testConsoleConcurrentOutputOrder(t *testing.T, laterPath string) {
	server, client := websocketpipe.Pair(t)
	first := "earlier-output\r\nroot@fixture:~# "
	later := "later-output\r\n"
	if laterPath == "display" {
		later = terminaltext.FormatAutomationCommand("echo later-output")
	}
	session := &managedConsoleSession{
		id: 7, status: "connected", manager: &Manager{}, workClosed: true,
		clients: map[*websocket.Conn]*sync.Mutex{server: {}},
		manualActive: &consoleSessionManualCapture{
			RequestID: 1, Command: "echo earlier-output", ResumePrompt: "root@fixture:~# ",
		},
	}
	// Pause the real prompt-completion producer after transcript append, before
	// delivery, at the existing owned-work admission boundary.
	session.workMu.Lock()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(session.workMu.Unlock) }
	firstDone, laterDone := make(chan struct{}), make(chan struct{})
	laterStarted := false
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
		release()
		waitConsoleFrameWork(t, firstDone)
		if laterStarted {
			waitConsoleFrameWork(t, laterDone)
		}
	})
	go func() { defer close(firstDone); session.appendSafeOutput(first) }()
	waitConsoleTranscript(t, session, first)
	session.mu.Lock()
	completedPrompt := session.manualActive == nil
	session.mu.Unlock()
	if !completedPrompt {
		t.Fatal("fixture did not reach prompt-completion admission")
	}
	// The first append must retain delivery ownership across its asynchronous
	// history admission boundary, not just while it holds the transcript mutex.
	if session.outputMu.TryLock() {
		session.outputMu.Unlock()
		t.Fatal("transcript append released delivery ownership before emitting its frame")
	}
	laterStarted = true
	go func() {
		defer close(laterDone)
		if laterPath == "manual" {
			session.appendSafeOutput(later)
		} else {
			session.appendDisplayOutput(later)
		}
	}()
	release()
	firstFrame := readConsoleFrame(t, client)
	if firstFrame.Type != "output" || firstFrame.Data != first {
		t.Fatalf("live delivery inverted transcript order: got %q, first transcript append was %q", firstFrame.Data, first)
	}
	secondFrame := readConsoleFrame(t, client)
	if secondFrame.Type != "output" || !strings.Contains(secondFrame.Data, "later-output") {
		t.Fatalf("later output frame changed: %#v", secondFrame)
	}
	waitConsoleFrameWork(t, firstDone)
	waitConsoleFrameWork(t, laterDone)
	_, transcript := session.snapshot()
	if firstFrame.Data+secondFrame.Data != transcript {
		t.Fatalf("live/transcript divergence: live=%q transcript=%q", firstFrame.Data+secondFrame.Data, transcript)
	}
}
