package foundation

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type failedOpenDatabaseSpy struct {
	failures atomic.Int64
	calls    atomic.Int64
}

func (spy *failedOpenDatabaseSpy) Close() error {
	spy.calls.Add(1)
	if spy.failures.Add(-1) >= 0 {
		return errors.New("injected database close failure")
	}
	return nil
}

type failedOpenOwnershipSpy struct {
	failures atomic.Int64
	calls    atomic.Int64
}

func (spy *failedOpenOwnershipSpy) Release() (bool, error) {
	spy.calls.Add(1)
	if spy.failures.Add(-1) >= 0 {
		return false, errors.New("injected ownership release failure")
	}
	return true, nil
}

func TestFailedOpenCleanupRetriesDatabaseAndOwnershipInOrder(t *testing.T) {
	database := &failedOpenDatabaseSpy{}
	database.failures.Store(1)
	ownership := &failedOpenOwnershipSpy{}
	ownership.failures.Store(1)
	resources := &failedOpenResources{database: database, ownership: ownership}
	if err := resources.Close(); err == nil {
		t.Fatal("expected transient cleanup errors to remain observable")
	}
	if database.calls.Load() != 2 || ownership.calls.Load() != 2 {
		t.Fatalf("cleanup calls: database=%d ownership=%d", database.calls.Load(), ownership.calls.Load())
	}
	closed, err := resources.closeOnce()
	if err != nil || !closed {
		t.Fatalf("cleanup did not remain idempotently closed: closed=%v err=%v", closed, err)
	}
}

func TestFailedOpenCleanupRetainsResourcesForBackgroundRetry(t *testing.T) {
	ownership := &failedOpenOwnershipSpy{}
	ownership.failures.Store(3)
	resources := &failedOpenResources{ownership: ownership}
	if err := resources.Close(); err == nil {
		t.Fatal("expected deferred cleanup error")
	}
	deadline := time.Now().Add(2 * time.Second)
	for ownership.calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ownership.calls.Load() < 4 {
		t.Fatalf("background cleanup did not retry ownership: calls=%d", ownership.calls.Load())
	}
	closed, err := resources.closeOnce()
	if err != nil || !closed {
		t.Fatalf("background cleanup did not release resources: closed=%v err=%v", closed, err)
	}
}
