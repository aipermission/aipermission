package commandrequests

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type fakeBulkRequestOwner struct {
	mu             sync.Mutex
	prepared       []Insert
	nextID         int64
	finish         chan Completion
	setSession     chan [2]int64
	finishedActive chan console.SessionHandle
}

func (owner *fakeBulkRequestOwner) Prepare(_ context.Context, insert Insert) (PreparedInsert, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.prepared = append(owner.prepared, insert)
	return PreparedInsert{insert: insert}, nil
}

func (owner *fakeBulkRequestOwner) InsertPrepared(context.Context, Executor, PreparedInsert) (int64, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.nextID++
	return owner.nextID, nil
}

func (owner *fakeBulkRequestOwner) SetSession(_ context.Context, requestID, sessionID int64) error {
	owner.setSession <- [2]int64{requestID, sessionID}
	return nil
}

func (owner *fakeBulkRequestOwner) Finish(_ context.Context, completion Completion) error {
	owner.finish <- completion
	return nil
}

func (owner *fakeBulkRequestOwner) FinishActive(_ context.Context, _ int64, _ executionprincipal.Principal, handle console.SessionHandle) {
	owner.finishedActive <- handle
}

func (*fakeBulkRequestOwner) RunWorker(run func(context.Context)) bool {
	go run(context.Background())
	return true
}

type fakeBulkSessions struct {
	result console.ExecResult
	err    error
	calls  chan int64
}

func (sessions *fakeBulkSessions) Exec(_ context.Context, _ executionprincipal.Principal, runtimeID int64, _ string) (console.ExecResult, error) {
	sessions.calls <- runtimeID
	return sessions.result, sessions.err
}

func newBulkTestRuntime(t *testing.T) (*BulkHTTPRuntime, *fakeBulkRequestOwner, *fakeBulkSessions) {
	t.Helper()
	principal, err := executionprincipal.LocalOperator("workspace", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeBulkRequestOwner{
		finish: make(chan Completion, BulkMaxTargets), setSession: make(chan [2]int64, BulkMaxTargets),
		finishedActive: make(chan console.SessionHandle, BulkMaxTargets),
	}
	sessions := &fakeBulkSessions{calls: make(chan int64, BulkMaxTargets)}
	runtime := &BulkHTTPRuntime{
		Requests: owner, Sessions: sessions,
		Principal: func() (executionprincipal.Principal, error) { return principal, nil },
		ResolveTarget: func(_ context.Context, runtimeID int64) (BulkTarget, error) {
			return BulkTarget{RuntimeID: runtimeID, Name: "target"}, nil
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, BulkAuditAppender) error) error {
			return mutate(nil, func(_ *sql.Tx, actor string, tokenID *int64, runtimeID int64, action string, payload any) error {
				if actor != "user" || tokenID != nil || runtimeID != 0 || action != "console.bulk_exec.started" {
					t.Fatalf("unexpected audit identity: %s %#v %d %s", actor, tokenID, runtimeID, action)
				}
				return nil
			})
		},
		InitialTimeout: time.Second,
	}
	return runtime, owner, sessions
}

func bulkRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/console/bulk-exec", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestBulkRunValidatesConfirmationBeforeResolvingTargets(t *testing.T) {
	runtime, _, _ := newBulkTestRuntime(t)
	resolveCalls := 0
	runtime.ResolveTarget = func(context.Context, int64) (BulkTarget, error) {
		resolveCalls++
		return BulkTarget{}, nil
	}
	handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handler.Run(response, bulkRequest(`{"target_ids":[1,2],"command":"hostname","confirmation":"wrong"}`))
	if response.Code != http.StatusBadRequest || resolveCalls != 0 || !strings.Contains(response.Body.String(), "RUN ON 2 TARGETS") {
		t.Fatalf("status=%d resolve calls=%d body=%s", response.Code, resolveCalls, response.Body.String())
	}
}

func TestBulkRunCreatesAtomicRequestsAndCompletesEachTarget(t *testing.T) {
	runtime, owner, sessions := newBulkTestRuntime(t)
	sessions.result = console.ExecResult{SessionID: 8, Output: "ok", ExitCode: 0}
	handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handler.Run(response, bulkRequest(`{"target_ids":[4,7],"command":" hostname ","reason":" smoke ","confirmation":"RUN ON 2 TARGETS"}`))
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"parallelism":3`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	owner.mu.Lock()
	prepared := append([]Insert(nil), owner.prepared...)
	owner.mu.Unlock()
	if len(prepared) != 2 || prepared[0].Command != "hostname" || prepared[0].Reason != "smoke" || prepared[0].Source != SourceManual {
		t.Fatalf("prepared requests = %#v", prepared)
	}
	for range 2 {
		select {
		case completion := <-owner.finish:
			if completion.Status != "completed" || completion.Stdout != "ok" || completion.SessionID != 8 {
				t.Fatalf("completion = %#v", completion)
			}
		case <-time.After(time.Second):
			t.Fatal("bulk execution did not complete")
		}
	}
}

func TestBulkRunDoesNotDispatchWhenTransactionFails(t *testing.T) {
	runtime, _, sessions := newBulkTestRuntime(t)
	runtime.WithTransaction = func(context.Context, func(*sql.Tx, BulkAuditAppender) error) error {
		return errors.New("audit unavailable")
	}
	handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handler.Run(response, bulkRequest(`{"target_ids":[4],"command":"hostname","confirmation":"RUN ON 1 TARGETS"}`))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case runtimeID := <-sessions.calls:
		t.Fatalf("unexpected dispatch to runtime %d", runtimeID)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestBulkRunHandsRunningCommandToActiveCompletion(t *testing.T) {
	runtime, owner, sessions := newBulkTestRuntime(t)
	sessions.result = console.ExecResult{SessionID: 13, Generation: 2, Running: true}
	handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
	response := httptest.NewRecorder()
	handler.Run(response, bulkRequest(`{"target_ids":[5],"command":"sleep 30","confirmation":"RUN ON 1 TARGETS"}`))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case pair := <-owner.setSession:
		if pair != [2]int64{1, 13} {
			t.Fatalf("session pair = %#v", pair)
		}
	case <-time.After(time.Second):
		t.Fatal("running session was not recorded")
	}
	select {
	case handle := <-owner.finishedActive:
		if handle.ID != 13 || handle.RuntimeID != 5 || handle.Generation != 2 {
			t.Fatalf("active handle = %#v", handle)
		}
	case <-time.After(time.Second):
		t.Fatal("running command was not handed off")
	}
}

func TestBulkRunMapsMissingAndMismatchedTargets(t *testing.T) {
	for name, testCase := range map[string]struct {
		resolve func(context.Context, int64) (BulkTarget, error)
		status  int
	}{
		"missing":  {func(context.Context, int64) (BulkTarget, error) { return BulkTarget{}, ErrBulkTargetNotFound }, http.StatusNotFound},
		"mismatch": {func(context.Context, int64) (BulkTarget, error) { return BulkTarget{RuntimeID: 99}, nil }, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, _, _ := newBulkTestRuntime(t)
			runtime.ResolveTarget = testCase.resolve
			handler := NewBulkHTTPHandlers(func(http.ResponseWriter) (*BulkHTTPRuntime, bool) { return runtime, true })
			response := httptest.NewRecorder()
			handler.Run(response, bulkRequest(`{"target_ids":[5],"command":"hostname","confirmation":"RUN ON 1 TARGETS"}`))
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
