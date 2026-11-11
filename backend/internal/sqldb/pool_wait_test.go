//go:build cgo

package sqldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestPoolWaitReportCapturesNativeHolderWithoutQueryOrArguments(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "test.db")
	database, err := sql.Open("sqlite3", databasePath+"?_key=FixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	var observed atomic.Bool
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(operation int, _, _, _ string) int {
			if operation == sqlite3.SQLITE_SELECT && observed.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	t.Cleanup(func() {
		released.Do(func() { close(release) })
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("held native query did not finish")
		}
	})
	go func() {
		var result int
		done <- database.QueryRowContext(t.Context(), "SELECT 42").Scan(&result)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("native query never entered the driver")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var value string
	err = database.QueryRowContext(ctx, "SELECT 'private-query-argument-never-log'").Scan(&value)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked connection error=%v", err)
	}
	var snapshot poolWaitSnapshot
	reporter := &PoolWaitReporter{emit: func(report poolWaitSnapshot) { snapshot = report }}
	reporter.Report(database, err)
	if snapshot.Open != 1 || snapshot.InUse != 1 || snapshot.Idle != 0 || snapshot.WaitCount == 0 {
		t.Fatalf("pool evidence=%+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-query-argument") || strings.Contains(string(encoded), "FixturePassword123") || strings.Contains(string(encoded), filepath.Dir(databasePath)) {
		t.Fatal("pool diagnostics contained query, argument, password or local path")
	}
	found := false
	for _, run := range snapshot.DatabaseCode {
		if run.Kind == "driver" && strings.Contains(strings.Join(run.Functions, ";"), "go-sqlcipher") {
			found = true
		}
	}
	if !found || len(snapshot.DatabaseCode) > 8 {
		t.Fatalf("native holder code evidence absent or unbounded: %+v", snapshot.DatabaseCode)
	}
	if len(encoded) > 40000 {
		t.Fatalf("pool evidence exceeded log budget: %d", len(encoded))
	}
}

func TestPoolWaitReportIgnoresOtherErrorsAndThrottlesConcurrentFailures(t *testing.T) {
	reporter := &PoolWaitReporter{emit: func(poolWaitSnapshot) { t.Fatal("unexpected report") }}
	for _, err := range []error{nil, context.Canceled, errors.New("private-error-never-log")} {
		reporter.Report(&sql.DB{}, err)
	}
	reporter.Report(nil, context.DeadlineExceeded)
	var admissions atomic.Int64
	var group sync.WaitGroup
	now := time.Now()
	for range 100 {
		group.Go(func() {
			if reporter.admit(now) {
				admissions.Add(1)
			}
		})
	}
	group.Wait()
	if admissions.Load() != 1 || reporter.admit(now.Add(29*time.Second)) || !reporter.admit(now.Add(30*time.Second)) {
		t.Fatalf("reporter throttle admissions=%d", admissions.Load())
	}
}
