package observability

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestDispatcherStopBeforeStartCompletes(t *testing.T) {
	dispatcher := NewDispatcher(openAuditDatabaseForLifecycleTest(t))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := dispatcher.Stop(ctx); err != nil {
		t.Fatalf("Stop() before Start() error = %v", err)
	}
	select {
	case <-dispatcher.done:
	default:
		t.Fatal("Stop() returned before the dispatcher goroutine exited")
	}
}

func TestDispatcherStopWaitsForOutstandingBatchOwnership(t *testing.T) {
	dispatcher := NewDispatcher(openAuditDatabaseForLifecycleTest(t))
	<-dispatcher.dispatchGate
	dispatcher.Start()
	dispatcher.Notify()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := dispatcher.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() while batch ownership is held error = %v, want deadline", err)
	}
	dispatcher.dispatchGate <- struct{}{}
	if err := dispatcher.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() after batch ownership release error = %v", err)
	}
	if _, err := dispatcher.DispatchOnce(t.Context()); !errors.Is(err, errDispatcherStopped) {
		t.Fatalf("DispatchOnce() after Stop() error = %v, want stopped", err)
	}
}

func openAuditDatabaseForLifecycleTest(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "audit.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
