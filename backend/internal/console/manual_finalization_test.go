package console

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestConsoleFinalDrainClosesLateManualCaptureBeforeRedactorDestruction(t *testing.T) {
	const canary = "final-drain-fixture-secret"
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE_VALUE", Value: []byte(canary)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(environment.Destroy)
	redactor, err := environment.ExactValueRedactor()
	if err != nil {
		t.Fatal(err)
	}
	session := &managedConsoleSession{manager: &Manager{}, environment: environment, exactRedactor: redactor}
	entered, release, destroyed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
	workDone := make(chan struct{})
	if !session.runOwnedWork(func() {
		defer close(workDone)
		close(entered)
		<-release
		transcript := session.redactForPersistence(canary)
		session.mu.Lock()
		session.rawTranscript = transcript
		session.manualActive = &consoleSessionManualCapture{RequestID: 1, Command: "echo $FIXTURE_VALUE"}
		session.manualPause = &consoleSessionManualPause{Reason: manualPromptNotDetected}
		session.mu.Unlock()
	}) {
		t.Fatal("fixture work was not admitted")
	}
	destroyStarted := false
	t.Cleanup(func() {
		releaseWork()
		waitConsoleHistoryWork(t, workDone)
		if destroyStarted {
			waitConsoleHistoryWork(t, destroyed)
		}
	})
	<-entered
	session.finish("closed", "")
	destroyStarted = true
	go func() { defer close(destroyed); session.destroySensitiveRuntime() }()
	waitConsoleHistoryAdmissionClosed(t, session)
	releaseWork()
	waitConsoleHistoryWork(t, destroyed)
	session.mu.Lock()
	active, paused, closed, transcript := session.manualActive, session.manualPause, session.exactRedactionClosed, session.rawTranscript
	session.mu.Unlock()
	if active != nil || paused != nil {
		t.Fatalf("late capture survived final drain: active=%#v paused=%#v", active, paused)
	}
	if !closed || strings.Contains(transcript, canary) || !strings.Contains(transcript, "[REDACTED VAULT VALUE]") {
		t.Fatalf("admitted work lost exact redaction before final drain: closed=%v transcript=%q", closed, transcript)
	}
}

func waitConsoleHistoryAdmissionClosed(t *testing.T, session *managedConsoleSession) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		session.workMu.Lock()
		closed := session.workClosed
		session.workMu.Unlock()
		if closed {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("owned-work admission did not close")
		case <-ticker.C:
		}
	}
}

func waitConsoleHistoryWork(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("console history fixture work did not finish")
	}
}
