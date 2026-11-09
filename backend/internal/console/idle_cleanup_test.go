package console

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/console/idlecleanup"
)

func TestIdleCleanupUsesExistingConditionalCloseAndWorkspaceLifecycle(t *testing.T) {
	database, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.lastActivity = time.Now().Add(-2 * time.Hour)
	var actions []string
	observe := func(_ context.Context, action string, _ any) error { actions = append(actions, action); return nil }
	principal := testExecutionPrincipal()
	manager.ConfigureIdleCleanup(principal.WorkspaceID, principal.RuntimeInstanceID, observe)
	service := manager.idleCleanup
	manager.ConfigureIdleCleanup(principal.WorkspaceID, principal.RuntimeInstanceID, observe)
	if manager.idleCleanup != service {
		t.Fatal("duplicated scheduler")
	}
	policy, err := manager.CleanupPolicy(t.Context())
	if err != nil || policy.Enabled || policy.IdleMinutes != 60 {
		t.Fatalf("%+v %v", policy, err)
	}
	runner := func(ctx context.Context, _ string, payload func() any, run func(*sql.Tx) error) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := run(tx); err != nil {
			return err
		}
		_ = payload()
		return tx.Commit()
	}
	if err := manager.UpdateCleanupPolicy(t.Context(), idlecleanup.Policy{Enabled: true, IdleMinutes: 60}, runner); err != nil {
		t.Fatal(err)
	}
	session.activeExec = &consoleSessionActiveExec{Command: "keep running"}
	if err := service.Cleanup(t.Context()); err != nil || session.closing || len(actions) != 0 {
		t.Fatal("closed running work", err)
	}
	session.activeExec = nil
	if err := service.Cleanup(t.Context()); err != nil || !session.closing || len(actions) != 2 {
		t.Fatalf("cleanup failed %v %v", err, actions)
	}
	if err := manager.CloseAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CleanupPolicy(t.Context()); !errors.Is(err, ErrManagerClosed) {
		t.Fatal(err)
	}
}

func TestIdleCleanupSkipsBusyRuntimeWithoutDelayingShutdown(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.lastActivity = time.Now().Add(-time.Hour)
	item := session.capacitySnapshot()
	lock := manager.runtimeLifecycle(session.runtimeID)
	lock.Lock()
	defer lock.Unlock()
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
		t.Fatal("cleanup blocked behind lifecycle lock")
	}
}

func TestCleanupProtectsUntrackedStartupBeforePublishingReady(t *testing.T) {
	for _, kind := range []string{"connector-work", "startup-input"} {
		t.Run(kind, func(t *testing.T) {
			_, manager, fixture := newManualHistoryTestSession(t)
			manager.openRuntime = func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
				s := &RuntimeSession{Stdin: &recordingWriteCloser{}, Output: make(chan RuntimeOutput), Done: testRuntimeDone(func() error { <-ctx.Done(); return ctx.Err() }), Close: func() error { return nil }}
				if kind == "connector-work" {
					s.UntrackedStartup = true
				} else {
					s.StartupInputAfterConnect = "sleep 7200\n"
				}
				return s, nil
			}
			t.Cleanup(func() { _ = manager.CloseAll(context.Background()) })
			record, err := manager.Create(t.Context(), CreateRequest{RuntimeID: fixture.runtimeID, Principal: testExecutionPrincipal(), WaitForStart: true})
			if err != nil {
				t.Fatal(err)
			}
			session := manager.active(record.ID)
			session.mu.Lock()
			session.lastActivity = time.Now().Add(-2 * time.Hour)
			session.mu.Unlock()
			item := session.capacitySnapshot()
			if item.CanClose || item.State != "untracked" {
				t.Fatalf("startup work classified idle: %+v", item)
			}
			if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 60); !errors.Is(err, ErrSessionChanged) {
				t.Fatal(err)
			}
		})
	}
}

func TestCleanupProtectsInterruptedWorkWithoutExitConfirmation(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	session.activeExec = &consoleSessionActiveExec{Command: "ignores SIGINT", Marker: "not-completed"}
	if err := session.interruptActiveCommand(t.Context()); err != nil {
		t.Fatal(err)
	}
	if session.activeCommand() != nil {
		t.Fatal("fixture did not exercise cleared tracking")
	}
	session.lastActivity = time.Now().Add(-2 * time.Hour)
	if session.capacitySnapshot().CanClose {
		t.Fatal("sending SIGINT was mistaken for confirmed completion")
	}
}
