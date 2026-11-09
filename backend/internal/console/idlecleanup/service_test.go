package idlecleanup

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/console/capacity"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func fixture(t *testing.T, ports Ports) (*Service, *sql.DB) {
	t.Helper()
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "cleanup.db"), "CleanupFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	s := New(database, ports)
	t.Cleanup(func() { _ = s.Stop(context.Background()); _ = database.Close() })
	return s, database
}

func mutate(database *sql.DB, fail error) auditedmutation.Runner {
	return func(ctx context.Context, action string, payload func() any, run func(*sql.Tx) error) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := run(tx); err != nil {
			return err
		}
		if action != "console.cleanup.policy_updated" || payload() == nil {
			return errors.New("invalid audit")
		}
		if fail != nil {
			return fail
		}
		return tx.Commit()
	}
}

func TestPolicyDefaultsValidationAndAtomicPersistence(t *testing.T) {
	s, database := fixture(t, Ports{})
	policy, err := s.Read(t.Context())
	if err != nil || policy.Enabled || policy.IdleMinutes != 60 {
		t.Fatalf("%+v %v", policy, err)
	}
	for _, minutes := range []int{10, 20, 30, 45, 60, 120, 180, 360} {
		policy = Policy{Enabled: true, IdleMinutes: minutes}
		if err := s.Update(t.Context(), policy, mutate(database, nil)); err != nil {
			t.Fatal(err)
		}
		read, err := New(database, Ports{}).Read(t.Context())
		if read != policy || err != nil {
			t.Fatalf("%+v %v", read, err)
		}
	}
	for _, minutes := range []int{-1, 0, 11, 100000} {
		if s.Update(t.Context(), Policy{Enabled: true, IdleMinutes: minutes}, mutate(database, nil)) == nil {
			t.Fatal("accepted invalid threshold")
		}
	}
	if s.Update(t.Context(), policy, nil) == nil {
		t.Fatal("accepted unaudited update")
	}
	failure := errors.New("audit unavailable")
	if err := s.Update(t.Context(), Policy{IdleMinutes: 60}, mutate(database, failure)); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	read, _ := s.Read(t.Context())
	if read != policy {
		t.Fatal("failed audit committed settings")
	}
	if _, err := database.Exec(`UPDATE settings SET value = '{"enabled":true,"idle_minutes":0}' WHERE key = ?`, policyKey); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context()); err == nil {
		t.Fatal("corrupt policy did not fail closed")
	}
}

func TestCleanupOptInSafetyThresholdAndAudit(t *testing.T) {
	var closed []int64
	var observed []string
	snapshotCalls := 0
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	ports := Ports{
		Snapshot: func(context.Context) (capacity.Report, error) {
			snapshotCalls++
			return capacity.Report{Items: []capacity.Session{
				{ID: 1, Generation: 1, LastActivity: old, CanClose: true},
				{ID: 2, Generation: 1, LastActivity: recent, CanClose: true},
				{ID: 3, Generation: 1, LastActivity: old, CanClose: false},
				{ID: 4, Generation: 2, LastActivity: old, CanClose: true},
				{ID: 5, Generation: 1, LastActivity: old, CanClose: true},
			}}, nil
		},
		Close: func(_ context.Context, item capacity.Selection, minutes int) (bool, error) {
			if minutes != 60 || item.LastActivity != old {
				t.Fatal("lost selection fence")
			}
			closed = append(closed, item.ID)
			if item.ID == 4 {
				return false, nil
			}
			if item.ID == 5 {
				return false, errors.New("finalization uncertain")
			}
			return true, nil
		},
		Observe: func(_ context.Context, action string, payload any) error {
			observed = append(observed, action)
			if action == "console.cleanup.automatic_completed" {
				want := capacity.CloseResult{Closed: []int64{1}, Skipped: []int64{4}, Uncertain: []int64{5}}
				if !reflect.DeepEqual(payload, want) {
					t.Fatalf("%+v", payload)
				}
			}
			return nil
		},
	}
	s, database := fixture(t, ports)
	if err := s.Cleanup(t.Context()); err != nil || snapshotCalls != 0 {
		t.Fatal("disabled cleanup scanned sessions", err)
	}
	if err := s.Update(t.Context(), Policy{Enabled: true, IdleMinutes: 60}, mutate(database, nil)); err != nil {
		t.Fatal(err)
	}
	if snapshotCalls != 0 {
		t.Fatal("save triggered immediate cleanup")
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closed, []int64{1, 4, 5}) || len(observed) != 2 {
		t.Fatalf("%v %v", closed, observed)
	}
	if err := s.Update(t.Context(), Policy{IdleMinutes: 30}, mutate(database, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(t.Context()); err != nil || snapshotCalls != 1 {
		t.Fatal("disabled policy ignored", err)
	}
}

func TestCleanupNoMutationWhenAuditFailsOrOperationIsBusy(t *testing.T) {
	called := false
	ports := Ports{
		Snapshot: func(context.Context) (capacity.Report, error) {
			return capacity.Report{Items: []capacity.Session{{ID: 1, Generation: 1, CanClose: true, LastActivity: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)}}}, nil
		},
		Close:   func(context.Context, capacity.Selection, int) (bool, error) { called = true; return true, nil },
		Observe: func(context.Context, string, any) error { return errors.New("audit failed") },
	}
	s, database := fixture(t, ports)
	if err := s.Update(t.Context(), Policy{Enabled: true, IdleMinutes: 30}, mutate(database, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(t.Context()); err == nil || called {
		t.Fatal("cleanup without audit intent", err)
	}
	s.operation.Lock()
	err := s.Cleanup(t.Context())
	s.operation.Unlock()
	if err != nil || called {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Cleanup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestHourlyBoundaryAndWorkerStops(t *testing.T) {
	for _, value := range []string{"2026-11-09T10:00:00Z", "2026-11-09T10:59:59.999+03:00", "2026-11-09T10:30:12.5+05:30"} {
		now, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatal(err)
		}
		next := nextHour(now)
		if !next.After(now) || next.Sub(now) > time.Hour || next.Minute() != 0 || next.Second() != 0 || next.Nanosecond() != 0 {
			t.Fatalf("%s -> %s", now, next)
		}
	}
	s, _ := fixture(t, Ports{})
	s.Start()
	s.Start()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	default:
		t.Fatal("worker leaked after stop")
	}
}

func TestCleanupCancellationAccountsForEveryIntendedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var result capacity.CloseResult
	ports := Ports{
		Snapshot: func(context.Context) (capacity.Report, error) {
			items := []capacity.Session{}
			for id := int64(1); id <= 3; id++ {
				items = append(items, capacity.Session{ID: id, Generation: 1, CanClose: true, LastActivity: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)})
			}
			return capacity.Report{Items: items}, nil
		},
		Close: func(context.Context, capacity.Selection, int) (bool, error) { cancel(); return false, context.Canceled },
		Observe: func(ctx context.Context, action string, payload any) error {
			if action == "console.cleanup.automatic_completed" {
				if ctx.Err() != nil {
					t.Fatal("outcome audit canceled")
				}
				result = payload.(capacity.CloseResult)
			}
			return nil
		},
	}
	s, database := fixture(t, ports)
	if err := s.Update(t.Context(), Policy{Enabled: true, IdleMinutes: 60}, mutate(database, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, capacity.CloseResult{Closed: []int64{}, Skipped: []int64{2, 3}, Uncertain: []int64{1}}) {
		t.Fatalf("incomplete batch outcome: %+v", result)
	}
}
