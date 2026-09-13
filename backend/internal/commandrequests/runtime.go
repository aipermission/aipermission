package commandrequests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

var ErrRuntimeUnavailable = errors.New("command request runtime is unavailable")

const (
	commandPersistenceAttempts = 5
	commandPersistenceDelay    = 100 * time.Millisecond
	commandOutcomeUnknown      = "remote command finished, but AIPermission could not persist its exact terminal result"
)

type Redactor func(context.Context, string) string

type ActiveSessions interface {
	WaitActive(context.Context, executionprincipal.Principal, console.SessionHandle) (console.ExecResult, error)
	InterruptActive(context.Context, executionprincipal.Principal, console.SessionHandle) error
}

type RuntimeDependencies struct {
	Store             *Store
	Codec             CommandCodec
	Projection        Projection
	Redact            Redactor
	Sessions          ActiveSessions
	BackgroundTimeout time.Duration
}

type Runtime struct {
	store             *Store
	codec             CommandCodec
	projection        Projection
	redact            Redactor
	sessions          ActiveSessions
	backgroundTimeout time.Duration
	workerMu          sync.Mutex
	workerCtx         context.Context
	workerCancel      context.CancelFunc
	workerWG          sync.WaitGroup
	workerDone        chan struct{}
	workerWait        sync.Once
	workersClosed     bool
	workerErrors      map[int64]error
}

func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Store == nil || dependencies.Codec == nil || dependencies.Projection == nil ||
		dependencies.Redact == nil || dependencies.Sessions == nil {
		return nil, ErrRuntimeUnavailable
	}
	timeout := dependencies.BackgroundTimeout
	if timeout <= 0 {
		return nil, ErrRuntimeUnavailable
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	return &Runtime{
		store: dependencies.Store, codec: dependencies.Codec, projection: dependencies.Projection,
		redact: dependencies.Redact, sessions: dependencies.Sessions, backgroundTimeout: timeout,
		workerCtx: workerCtx, workerCancel: workerCancel, workerDone: make(chan struct{}),
	}, nil
}

// RunWorker admits one workspace-owned background command worker. It returns
// false once shutdown has begun, so no worker can outlive encrypted storage.
func (r *Runtime) RunWorker(run func(context.Context)) bool {
	if r == nil || run == nil {
		return false
	}
	r.workerMu.Lock()
	if r.workersClosed || r.workerCtx == nil {
		r.workerMu.Unlock()
		return false
	}
	ctx := r.workerCtx
	r.workerWG.Add(1)
	r.workerMu.Unlock()
	go func() {
		defer r.workerWG.Done()
		run(ctx)
	}()
	return true
}

// BeginWorkerShutdown prevents new workers and cancels active work without
// waiting for command persistence to finish.
func (r *Runtime) BeginWorkerShutdown() {
	if r == nil {
		return
	}
	r.workerMu.Lock()
	r.workersClosed = true
	cancel := r.workerCancel
	r.workerCancel = nil
	r.workerMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitWorkers waits until command workers can no longer access workspace
// storage. Repeated waits observe the same drain signal.
func (r *Runtime) WaitWorkers(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.workerWait.Do(func() {
		go func() {
			r.workerWG.Wait()
			close(r.workerDone)
		}()
	})
	select {
	case <-r.workerDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StopWorkers closes admission, cancels active workers, and waits for all
// command persistence to finish before the workspace database can close.
func (r *Runtime) StopWorkers(ctx context.Context) error {
	r.BeginWorkerShutdown()
	return r.WaitWorkers(ctx)
}

func (r *Runtime) Prepare(ctx context.Context, request Insert) (PreparedInsert, error) {
	if err := r.validate(); err != nil {
		return PreparedInsert{}, err
	}
	return PreparedInsert{
		insert:        request,
		storedCommand: r.redact(ctx, request.Command),
		storedReason:  r.redact(ctx, request.Reason),
	}, nil
}

func (r *Runtime) Insert(ctx context.Context, request Insert) (int64, error) {
	prepared, err := r.Prepare(ctx, request)
	if err != nil {
		return 0, err
	}
	return r.store.Insert(ctx, r.codec, r.projection, prepared)
}

func (r *Runtime) InsertPrepared(
	ctx context.Context,
	executor Executor,
	request PreparedInsert,
) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	return r.store.InsertWithExecutor(ctx, executor, r.codec, r.projection, request)
}

func (r *Runtime) ExecutionCommand(ctx context.Context, id int64) (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.store.ExecutionCommand(ctx, r.codec, id)
}

func (r *Runtime) Get(ctx context.Context, id, tokenID int64, source string) (Record, error) {
	if err := r.validate(); err != nil {
		return Record{}, err
	}
	return r.store.Get(ctx, id, tokenID, source)
}

func (r *Runtime) SetSession(ctx context.Context, id, sessionID int64) error {
	if err := r.validate(); err != nil {
		return err
	}
	err := r.retryPersistence(ctx, func(attempt context.Context) error {
		return r.store.SetSession(attempt, r.projection, id, sessionID)
	})
	if err != nil {
		r.recordWorkerError(id, fmt.Errorf("persist command session %d: %w", id, err))
	}
	return err
}

func (r *Runtime) Finish(ctx context.Context, completion Completion) error {
	if err := r.validate(); err != nil {
		return err
	}
	completion.Stdout = r.redact(ctx, console.PlainOutput(completion.Stdout))
	completion.Stderr = r.redact(ctx, console.PlainOutput(completion.Stderr))
	completion.Error = r.redact(ctx, completion.Error)
	err := r.persistCompletion(ctx, completion)
	if err != nil {
		r.recordWorkerError(completion.ID, fmt.Errorf("persist command completion %d: %w", completion.ID, err))
	} else {
		r.clearWorkerError(completion.ID)
	}
	return err
}

func (r *Runtime) FinishActive(parent context.Context, requestID int64, principal executionprincipal.Principal, handle console.SessionHandle) {
	if r.validate() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, r.backgroundTimeout)
	defer cancel()
	result, err := r.sessions.WaitActive(ctx, principal, handle)
	if err != nil {
		// Workspace shutdown owns cancellation recovery after every command
		// worker has drained. Do not turn its cancellation into a timeout or
		// prevent the coordinator from recording the canonical shutdown result.
		if parent.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_ = r.sessions.InterruptActive(cleanup, principal, handle)
			_ = r.Finish(cleanup, Completion{ID: requestID, Status: "error", Error: "command timed out while running in background"})
			return
		}
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = r.Finish(cleanup, Completion{ID: requestID, Status: "error", Error: err.Error()})
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()
	_ = r.Finish(cleanup, Completion{
		ID: requestID, Status: status, SessionID: result.SessionID,
		Stdout: result.Output, ExitCode: result.ExitCode,
	})
}

func (r *Runtime) persistCompletion(ctx context.Context, completion Completion) error {
	err := r.retryPersistence(ctx, func(attempt context.Context) error {
		err := r.store.Finish(attempt, r.projection, completion)
		if !errors.Is(err, ErrNotRunning) {
			return err
		}
		status, statusErr := r.store.Status(attempt, completion.ID)
		if statusErr == nil && status == completion.Status {
			return nil
		}
		return errors.Join(err, statusErr)
	})
	if err == nil {
		return nil
	}
	unknown := Completion{
		ID: completion.ID, Status: "outcome_unknown", SessionID: completion.SessionID,
		Error: commandOutcomeUnknown,
	}
	unknownErr := r.retryPersistence(ctx, func(attempt context.Context) error {
		err := r.store.Finish(attempt, r.projection, unknown)
		if !errors.Is(err, ErrNotRunning) {
			return err
		}
		status, statusErr := r.store.Status(attempt, completion.ID)
		if statusErr == nil && (status == completion.Status || status == unknown.Status) {
			return nil
		}
		return errors.Join(err, statusErr)
	})
	if unknownErr == nil {
		return nil
	}
	return errors.Join(err, unknownErr)
}

func (r *Runtime) retryPersistence(parent context.Context, operation func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < commandPersistenceAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
		err := operation(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt+1 < commandPersistenceAttempts {
			time.Sleep(commandPersistenceDelay)
		}
	}
	return lastErr
}

func (r *Runtime) recordWorkerError(requestID int64, err error) {
	if err == nil {
		return
	}
	r.workerMu.Lock()
	if r.workerErrors == nil {
		r.workerErrors = make(map[int64]error)
	}
	r.workerErrors[requestID] = err
	r.workerMu.Unlock()
}

func (r *Runtime) clearWorkerError(requestID int64) {
	r.workerMu.Lock()
	delete(r.workerErrors, requestID)
	r.workerMu.Unlock()
}

func (r *Runtime) hasWorkerErrors() bool {
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	return len(r.workerErrors) > 0
}

func (r *Runtime) CancelRunning(ctx context.Context, errorText string) error {
	if err := r.validate(); err != nil {
		return err
	}
	var err error
	if r.hasWorkerErrors() {
		err = r.store.MarkRunningOutcomeUnknown(ctx, r.projection, r.redact(ctx, commandOutcomeUnknown))
	} else {
		err = r.store.CancelRunning(ctx, r.projection, r.redact(ctx, errorText))
	}
	return ignoreClosedDatabase(err)
}

func (r *Runtime) CancelRunningForSession(ctx context.Context, sessionID int64, errorText string) error {
	if err := r.validate(); err != nil {
		return err
	}
	if sessionID < 1 {
		return nil
	}
	err := r.store.CancelRunningForSession(ctx, r.projection, sessionID, r.redact(ctx, errorText))
	return ignoreClosedDatabase(err)
}

func (r *Runtime) CancelRunningForRuntime(ctx context.Context, runtimeID int64, errorText string) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	if runtimeID < 1 {
		return 0, nil
	}
	affected, err := r.store.CancelRunningForRuntime(ctx, r.projection, runtimeID, r.redact(ctx, errorText))
	err = ignoreClosedDatabase(err)
	if err == nil {
		return affected, nil
	}
	return 0, err
}

func (r *Runtime) validate() error {
	if r == nil || r.store == nil || r.codec == nil || r.projection == nil ||
		r.redact == nil || r.sessions == nil || r.backgroundTimeout <= 0 {
		return ErrRuntimeUnavailable
	}
	return nil
}

func ignoreClosedDatabase(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "database is closed") {
		return nil
	}
	return err
}
