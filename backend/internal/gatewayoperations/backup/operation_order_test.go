package backup

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type lifecycleStub struct {
	acquire         func(context.Context) (func(), error)
	acquireMutation func(context.Context) (func(), error)
}

type operationOrderRuntime struct{ identity workspacelifecycle.Identity }

func (runtime *operationOrderRuntime) WorkspaceIdentity() workspacelifecycle.Identity {
	return runtime.identity
}

func (*operationOrderRuntime) WorkspaceDatabase() *sql.DB { return nil }

func newOperationOrderLifecycle(t *testing.T) *workspacelifecycle.Service[*operationOrderRuntime] {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.aipdb")
	service, err := workspacelifecycle.NewService(workspacelifecycle.Dependencies[*operationOrderRuntime]{
		DataPath: path,
		Registry: workspacelifecycle.NewRegistry(path, "default", func(runtime *operationOrderRuntime) workspacelifecycle.Identity {
			return runtime.identity
		}),
		Open: func(context.Context, string, string, string) (*operationOrderRuntime, error) {
			return nil, errors.New("unused")
		},
		Close: func(*operationOrderRuntime) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func (l lifecycleStub) AcquireReadContext(ctx context.Context) (func(), error) {
	return l.acquire(ctx)
}

func (l lifecycleStub) AcquireMutationContext(ctx context.Context) (func(), error) {
	if l.acquireMutation != nil {
		return l.acquireMutation(ctx)
	}
	return l.acquire(ctx)
}

func (lifecycleStub) Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	panic("not used")
}

func TestAcquireReadOperationUsesBackupSlotBeforeLifecycle(t *testing.T) {
	events := []string{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			events = append(events, "lifecycle.acquire")
			return func() { events = append(events, "lifecycle.release") }, nil
		}},
		AcquireOperation: func(context.Context) (func(), error) {
			events = append(events, "operation.acquire")
			return func() { events = append(events, "operation.release") }, nil
		},
	})
	lease, err := component.acquireReadOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease.ReleaseLifecycle()
	lease.Release()
	want := []string{"operation.acquire", "lifecycle.acquire", "lifecycle.release", "operation.release"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lease order = %v, want %v", events, want)
	}
}

func TestAuthorizedOperationRevalidatesWorkspaceAfterLifecycleAdmission(t *testing.T) {
	events := []string{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			events = append(events, "lifecycle.acquire")
			return func() { events = append(events, "lifecycle.release") }, nil
		}},
		AcquireOperation: func(context.Context) (func(), error) {
			events = append(events, "operation.acquire")
			return func() { events = append(events, "operation.release") }, nil
		},
		AuthorizeOperation: func(w http.ResponseWriter, _ *http.Request) bool {
			events = append(events, "workspace.authorize")
			http.Error(w, "workspace changed", http.StatusConflict)
			return false
		},
	})
	response := httptest.NewRecorder()
	lease, ok := component.authorizedReadOperation(response, httptest.NewRequest(http.MethodGet, "/api/backup/download", nil))
	if ok || lease != nil || response.Code != http.StatusConflict {
		t.Fatalf("authorization result: ok=%t lease=%v status=%d", ok, lease, response.Code)
	}
	want := []string{"operation.acquire", "lifecycle.acquire", "workspace.authorize", "lifecycle.release", "operation.release"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("authorization order = %v, want %v", events, want)
	}
}

func TestContendedBackupOperationRejectsWorkspaceChangedWhileWaiting(t *testing.T) {
	var lifecycleMu sync.RWMutex
	var current atomic.Value
	current.Store("workspace-a")
	thirdAttempted := make(chan struct{})
	var operationAcquires atomic.Int32
	limiter := &OperationLimiter{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			lifecycleMu.RLock()
			return lifecycleMu.RUnlock, nil
		}},
		AcquireOperation: func(ctx context.Context) (func(), error) {
			if operationAcquires.Add(1) == 3 {
				close(thirdAttempted)
			}
			return limiter.Acquire(ctx)
		},
		AuthorizeOperation: func(w http.ResponseWriter, r *http.Request) bool {
			if r.Header.Get("X-AIPermission-Workspace") != current.Load().(string) {
				http.Error(w, "workspace changed", http.StatusConflict)
				return false
			}
			return true
		},
	})
	first, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.ReleaseLifecycle()
	second.ReleaseLifecycle()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/upload", nil)
	request.Header.Set("X-AIPermission-Workspace", "workspace-a")
	done := make(chan bool, 1)
	go func() {
		lease, ok := component.authorizedReadOperation(response, request)
		if lease != nil {
			lease.Release()
		}
		done <- ok
	}()
	select {
	case <-thirdAttempted:
	case <-time.After(time.Second):
		t.Fatal("contended operation did not reach the operation limiter")
	}
	lifecycleMu.Lock()
	current.Store("workspace-b")
	lifecycleMu.Unlock()
	first.Release()
	select {
	case ok := <-done:
		if ok || response.Code != http.StatusConflict {
			t.Fatalf("stale operation: ok=%t status=%d", ok, response.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("contended operation did not finish after a slot was released")
	}
	second.Release()
}

func TestAcquireReadOperationReleasesSlotWhenLifecycleAdmissionFails(t *testing.T) {
	wantErr := errors.New("workspace is locking")
	released := false
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			return nil, wantErr
		}},
		AcquireOperation: func(context.Context) (func(), error) {
			return func() { released = true }, nil
		},
	})
	if _, err := component.acquireReadOperation(t.Context()); !errors.Is(err, wantErr) {
		t.Fatalf("acquireReadOperation() error = %v, want %v", err, wantErr)
	}
	if !released {
		t.Fatal("operation slot was retained after lifecycle admission failed")
	}
}

func TestContendedBackupSlotDoesNotBlockExclusiveLifecycleTransition(t *testing.T) {
	var lifecycleMu sync.RWMutex
	var readAcquires atomic.Int32
	var operationAcquires atomic.Int32
	thirdAttempted := make(chan struct{})
	limiter := &OperationLimiter{}
	component := New(Dependencies{
		Lifecycle: lifecycleStub{acquire: func(context.Context) (func(), error) {
			lifecycleMu.RLock()
			readAcquires.Add(1)
			return lifecycleMu.RUnlock, nil
		}},
		AcquireOperation: func(ctx context.Context) (func(), error) {
			if operationAcquires.Add(1) == 3 {
				close(thirdAttempted)
			}
			return limiter.Acquire(ctx)
		},
	})
	first, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()

	thirdResult := make(chan *lifecycleOperationLease, 1)
	thirdError := make(chan error, 1)
	go func() {
		lease, acquireErr := component.acquireReadOperation(t.Context())
		if acquireErr != nil {
			thirdError <- acquireErr
			return
		}
		thirdResult <- lease
	}()
	select {
	case <-thirdAttempted:
	case <-time.After(time.Second):
		t.Fatal("third operation did not reach the contended slot")
	}
	if got := readAcquires.Load(); got != 2 {
		t.Fatalf("contended operation acquired lifecycle read lock %d times, want 2", got)
	}

	first.ReleaseLifecycle()
	second.ReleaseLifecycle()
	exclusive := make(chan struct{})
	go func() {
		lifecycleMu.Lock()
		close(exclusive)
		lifecycleMu.Unlock()
	}()
	select {
	case <-exclusive:
	case <-time.After(time.Second):
		t.Fatal("exclusive lifecycle transition was blocked by a request waiting for an operation slot")
	}

	first.Release()
	select {
	case lease := <-thirdResult:
		lease.Release()
	case err := <-thirdError:
		t.Fatalf("third operation failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("third operation did not acquire the released slot")
	}
}

func TestRealLifecycleAndLimiterCannotInvertOperationLockOrder(t *testing.T) {
	lifecycle := newOperationOrderLifecycle(t)
	limiter := &OperationLimiter{}
	component := New(Dependencies{Lifecycle: lifecycle, AcquireOperation: limiter.Acquire})

	first, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := component.acquireReadOperation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()

	mutationAcquired := make(chan *lifecycleOperationLease, 1)
	go func() {
		lease, acquireErr := component.acquireMutationOperation(t.Context())
		if acquireErr == nil {
			mutationAcquired <- lease
		}
	}()
	select {
	case lease := <-mutationAcquired:
		lease.Release()
		t.Fatal("mutation operation bypassed the saturated operation limiter")
	case <-time.After(25 * time.Millisecond):
	}

	first.ReleaseLifecycle()
	second.ReleaseLifecycle()
	probeRelease, err := lifecycle.AcquireMutationContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	probeRelease()

	first.Release()
	select {
	case lease := <-mutationAcquired:
		lease.Release()
	case <-time.After(time.Second):
		t.Fatal("mutation operation did not acquire after an operation slot was released")
	}
}
