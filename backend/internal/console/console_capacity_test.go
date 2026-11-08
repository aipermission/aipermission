package console

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func TestCapacitySnapshotsAndWatch(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	session.lastActivity = time.Now().Add(-time.Hour)
	session.transcript = "NEVER_EXPOSE_TRANSCRIPT"
	manager.sessions[session.id] = session
	report, err := manager.Capacity(t.Context())
	if err != nil || report.Used != 1 || report.Limit != 32 || report.Available != 31 || report.Items[0].TargetName != "worker-1" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), "NEVER_EXPOSE_TRANSCRIPT") {
		t.Fatal("capacity leaked output")
	}
	if !report.Items[0].Eligible(time.Now(), 30) {
		t.Fatal("idle shell not selectable")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := manager.WaitCapacity(ctx, report.Revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("watch ignored cancellation: %v", err)
	}
	ready := make(chan uint64, 1)
	go func() { next, _ := manager.WaitCapacity(t.Context(), report.Revision); ready <- next }()
	manager.capacityChanged()
	select {
	case next := <-ready:
		if next <= report.Revision {
			t.Fatal("watch lost change")
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not wake")
	}
	manager.closed = true
	if _, err := manager.Capacity(t.Context()); !errors.Is(err, ErrManagerClosed) {
		t.Fatal(err)
	}
}

func TestCapacityProtectedStatesAndDrift(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	session.lastActivity = time.Now().Add(-time.Hour)
	manager.sessions[session.id] = session
	item := session.capacitySnapshot()
	selection := capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}
	token, _ := executionprincipal.MCPToken(1, "test-workspace", "test-runtime")
	if err := manager.CloseCapacitySession(t.Context(), token, selection, 30); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), selection, 11); !errors.Is(err, ErrSessionChanged) {
		t.Fatal(err)
	}
	for _, kind := range []string{"running", "manual", "untracked", "partial", "connecting", "closing", "recent", "generation"} {
		t.Run(kind, func(t *testing.T) {
			session.activeExec, session.manualActive, session.manualPause = nil, nil, nil
			session.manualInput.Reset()
			session.status, session.closing = "connected", false
			session.lastActivity = time.Now().Add(-time.Hour)
			selection.LastActivity = session.lastActivity.UTC().Format(time.RFC3339Nano)
			selection.Generation = session.generation
			switch kind {
			case "running":
				session.activeExec = &consoleSessionActiveExec{}
			case "manual":
				session.manualActive = &consoleSessionManualCapture{}
			case "untracked":
				session.manualPause = &consoleSessionManualPause{}
			case "partial":
				session.manualInput.Consume("unfinished")
			case "connecting":
				session.status = "connecting"
			case "closing":
				session.closing = true
			case "recent":
				session.lastActivity = time.Now()
			case "generation":
				selection.Generation++
			}
			if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), selection, 30); !errors.Is(err, ErrSessionChanged) {
				t.Fatalf("protected %s: %v", kind, err)
			}
		})
	}
}

func TestCapacityCleanupClaimRejectsNewInput(t *testing.T) {
	_, manager, session := newManualHistoryTestSession(t)
	session.lastActivity = time.Now().Add(-time.Hour)
	manager.sessions[session.id] = session
	session.stdin = &recordingWriteCloser{}
	item := session.capacitySnapshot()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manager.CloseCapacitySession(ctx, testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 30); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if session.closing {
		t.Fatal("canceled request claimed session")
	}
	if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 30); err != nil {
		t.Fatal(err)
	}
	if !session.closing || session.status != "closing" {
		t.Fatal("cleanup did not claim closing")
	}
	if _, err := session.writeInputAttempt("must not run"); err == nil {
		t.Fatal("closing shell accepted input")
	}
}

func TestCapacityTracksActualCreateAndFinalizedClose(t *testing.T) {
	_, manager, fixture := newManualHistoryTestSession(t)
	manager.openRuntime = func(ctx context.Context, _ RuntimeOpenRequest) (*RuntimeSession, error) {
		return &RuntimeSession{Stdin: &recordingWriteCloser{}, Output: make(chan RuntimeOutput), Done: testRuntimeDone(func() error { <-ctx.Done(); return ctx.Err() }), Close: func() error { return nil }}, nil
	}
	t.Cleanup(func() { _ = manager.CloseAll(context.Background()) })
	before, err := manager.Capacity(t.Context())
	if err != nil || before.Used != 0 {
		t.Fatalf("%+v %v", before, err)
	}
	created, err := manager.Create(t.Context(), CreateRequest{RuntimeID: fixture.runtimeID, Principal: testExecutionPrincipal(), WaitForStart: true})
	if err != nil {
		t.Fatal(err)
	}
	after, err := manager.Capacity(t.Context())
	if err != nil || after.Used != 1 || after.Revision <= before.Revision {
		t.Fatalf("%+v %v", after, err)
	}
	item := after.Items[0]
	if item.ID != created.ID || !item.CanClose {
		t.Fatalf("%+v", item)
	}
	if err := manager.CloseCapacitySession(t.Context(), testExecutionPrincipal(), capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity}, 0); err != nil {
		t.Fatal(err)
	}
	closed, err := manager.Capacity(t.Context())
	if err != nil || closed.Used != 0 || closed.Available != 32 {
		t.Fatalf("%+v %v", closed, err)
	}
}
