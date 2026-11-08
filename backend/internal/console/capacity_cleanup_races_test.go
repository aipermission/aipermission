package console

import (
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
)

func TestCapacityBusyInputIsSkippedWithoutWaiting(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	session.lastActivity = time.Now().Add(-time.Hour)
	manager.sessions[session.id] = session
	item := session.capacitySnapshot()
	session.inputMu.Lock()
	defer session.inputMu.Unlock()
	result := make(chan error, 1)
	go func() {
		result <- manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 30)
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrSessionChanged) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup waited on busy input")
	}
}

func TestCapacityProtectsCompoundInteractiveInputEvenAfterIntermediatePrompt(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	session.rawTranscript = "root@worker:~# "
	if err := session.submitManualInput("pwd\nnano notes\n"); err != nil {
		t.Fatal(err)
	}
	session.lastActivity = time.Now().Add(-time.Hour)
	if session.capacitySnapshot().CanClose {
		t.Fatal("compound input was mistaken for an idle shell")
	}
	session.appendOutput("editor screen without a shell prompt\n")
	if session.capacitySnapshot().CanClose {
		t.Fatal("editor output cleared uncertainty")
	}
	session.appendOutput("\nroot@worker:~# ")
	if session.capacitySnapshot().CanClose {
		t.Fatal("intermediate prompt cleared batch uncertainty")
	}
}

func TestCapacityManualHistoryCompletionDoesNotAuthorizeCleanup(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	session.rawTranscript = "root@worker:~# "
	if err := session.submitManualInput("cd /tmp\n"); err != nil {
		t.Fatal(err)
	}
	before, _ := manager.WaitCapacity(t.Context(), 0)
	session.appendOutput("\nroot@worker:/tmp# ")
	if session.manualActive != nil {
		t.Fatal("best-effort manual history did not recognize the changed prompt")
	}
	if session.capacitySnapshot().CanClose {
		t.Fatal("best-effort manual history completion authorized cleanup")
	}
	after, _ := manager.WaitCapacity(t.Context(), 0)
	if after <= before {
		t.Fatal("manual completion did not notify capacity watchers")
	}
}

func TestCapacityProtectsManualWorkWhenHistoryPersistenceFails(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.submitManualInput("sleep 3600\n"); err != nil {
		t.Fatal(err)
	}
	session.lastActivity = time.Now().Add(-time.Hour)
	if !session.cleanupUntracked || session.capacitySnapshot().CanClose {
		t.Fatal("history failure exposed dispatched work to idle cleanup")
	}
}
