package commandrequests

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestUnknownCompletionPersistenceRecoveryPreservesHandleAndGuidance(t *testing.T) {
	for _, failures := range []int{commandPersistenceAttempts, commandPersistenceAttempts * 2} {
		t.Run(strconv.Itoa(failures)+"-projection-failures", func(t *testing.T) {
			database, runtimeID := commandRequestFixture(t)
			projection := &transientCommandProjection{}
			owner := newTestRuntime(t, database, &testActiveSessions{}, projection)
			id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "remote effect", Status: "running"})
			if err != nil {
				t.Fatal(err)
			}
			projection.failures = failures
			err = owner.Finish(t.Context(), unknownCommandCompletion(id, 44, "observation denied"))
			if failures == commandPersistenceAttempts*2 {
				if err == nil {
					t.Fatal("exhausted persistence attempts reported success")
				}
				if err := owner.CancelRunning(t.Context(), "workspace closed"); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			record, err := owner.Get(t.Context(), id, 0, "")
			if err != nil || record.Status != "outcome_unknown" || record.SessionID == nil || *record.SessionID != 44 || record.Error != "observation denied; "+commandObservationUnknown {
				t.Fatalf("recovered uncertainty lost identity/guidance: %#v, %v", record, err)
			}
			if owner.hasWorkerErrors() || len(owner.pendingCompletions) != 0 {
				t.Fatal("successful recovery retained failed completion state")
			}
		})
	}
}

func TestPendingRecoveryRetainsFailedBindingAndRespectsTerminalWinner(t *testing.T) {
	for _, variant := range []string{"binding", "recovery-failure", "terminal-winner"} {
		t.Run(variant, func(t *testing.T) {
			database, runtimeID := commandRequestFixture(t)
			projection := &transientCommandProjection{}
			owner := newTestRuntime(t, database, &testActiveSessions{}, projection)
			id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "remote effect", Status: "running"})
			if err != nil {
				t.Fatal(err)
			}
			projection.failures = commandPersistenceAttempts * 2
			if variant == "binding" {
				projection.failures = commandPersistenceAttempts
				err = owner.SetSession(t.Context(), id, 44)
			} else {
				err = owner.Finish(t.Context(), unknownCommandCompletion(id, 44, ""))
			}
			if err == nil {
				t.Fatal("exhausted persistence attempts reported success")
			}
			if variant == "recovery-failure" {
				projection.failures = 1
				if err := owner.CancelRunning(t.Context(), "workspace closed"); err == nil {
					t.Fatal("failed recovery was silently acknowledged")
				}
				if len(owner.pendingCompletions) != 1 {
					t.Fatal("failed recovery discarded the pending handle")
				}
			}
			if variant == "terminal-winner" {
				if err := owner.store.Finish(t.Context(), projection, Completion{ID: id, Status: "completed", SessionID: 99, Stdout: "terminal winner"}); err != nil {
					t.Fatal(err)
				}
			}
			otherID, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "unrelated shutdown request", Status: "running"})
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.CancelRunning(t.Context(), "workspace closed"); err != nil {
				t.Fatal(err)
			}
			record, err := owner.Get(t.Context(), id, 0, "")
			status, sessionID, output := "outcome_unknown", int64(44), ""
			if variant == "terminal-winner" {
				status, sessionID, output = "completed", 99, "terminal winner"
			}
			if err != nil || record.Status != status || record.SessionID == nil || *record.SessionID != sessionID || record.Stdout != output {
				t.Fatalf("recovery lost pending identity or overwrote terminal winner: %#v, %v", record, err)
			}
			if owner.hasWorkerErrors() || len(owner.pendingCompletions) != 0 {
				t.Fatal("recovery did not clear retained failures")
			}
			other, err := owner.Get(t.Context(), otherID, 0, "")
			if err != nil || other.Status != "outcome_unknown" || !strings.Contains(other.Error, "workspace closed") {
				t.Fatalf("scoped recovery relabeled unrelated shutdown: %#v, %v", other, err)
			}
		})
	}
}

func TestBackgroundRealDeadlineAndFailedInterruptDoNotConfirmCompletion(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	sessions := &testActiveSessions{
		wait: func(ctx context.Context) (console.ExecResult, error) {
			<-ctx.Done()
			return console.ExecResult{}, ctx.Err()
		},
		interruptErr: errors.New("transport closed"),
	}
	owner := newTestRuntime(t, database, sessions, &testCommandProjection{})
	owner.backgroundTimeout = 10 * time.Millisecond
	id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "remote effect", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	owner.FinishActive(t.Context(), id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 3})
	record, err := owner.Get(t.Context(), id, 0, "")
	if err != nil || record.Status != "outcome_unknown" || record.SessionID == nil || *record.SessionID != 44 || sessions.interrupts != 1 || !strings.Contains(record.Error, "could not be interrupted") {
		t.Fatalf("failed interrupt falsely confirmed completion: %#v, %v", record, err)
	}
}
