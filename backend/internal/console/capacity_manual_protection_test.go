package console

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
)

func TestCapacityDoesNotTreatManualPromptOutputAsVerifiedExit(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	t.Cleanup(session.drainOwnedWork)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	session.rawTranscript = "root@worker:~# "
	if err := session.submitManualInput("printf 'root@worker:~# '; sleep 7200\n"); err != nil {
		t.Fatal(err)
	}
	session.appendOutput("root@worker:~# ")
	session.lastActivity = time.Now().Add(-2 * time.Hour)
	item := session.capacitySnapshot()
	if item.CanClose {
		t.Fatal("prompt-shaped process output was treated as verified exit")
	}
	if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 60); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("manual work admitted to cleanup: %v", err)
	}
}

func TestCapacityProtectsManualWorkAfterAbandonedAutomation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorization denied", true: "canceled before dispatch"}[canceled], func(t *testing.T) {
			_, manager, session := newManualHistoryTestSession(t)
			t.Cleanup(session.drainOwnedWork)
			manager.sessions[session.id] = session
			session.stdin = &recordingWriteCloser{}
			session.rawTranscript = "root@worker:~# "
			if err := session.submitManualInput("sleep 7200\n"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := session.execCommand(ctx, "true", func(write func() error) error {
				if !canceled {
					return ErrUnauthorized
				}
				cancel()
				return write()
			})
			if err == nil || session.activeCommand() != nil {
				t.Fatal("fixture did not clear an abandoned automation attempt", err)
			}
			session.lastActivity = time.Now().Add(-2 * time.Hour)
			if session.capacitySnapshot().CanClose {
				t.Fatal("abandoned automation removed protection for unfinished manual work")
			}
		})
	}
}
