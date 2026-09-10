package commandrequests

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

var ErrRuntimeUnavailable = errors.New("command request runtime is unavailable")

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
	return &Runtime{
		store: dependencies.Store, codec: dependencies.Codec, projection: dependencies.Projection,
		redact: dependencies.Redact, sessions: dependencies.Sessions, backgroundTimeout: timeout,
	}, nil
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
	return r.store.SetSession(ctx, r.projection, id, sessionID)
}

func (r *Runtime) Finish(ctx context.Context, completion Completion) error {
	if err := r.validate(); err != nil {
		return err
	}
	completion.Stdout = r.redact(ctx, console.PlainOutput(completion.Stdout))
	completion.Stderr = r.redact(ctx, console.PlainOutput(completion.Stderr))
	completion.Error = r.redact(ctx, completion.Error)
	return r.store.Finish(ctx, r.projection, completion)
}

func (r *Runtime) FinishActive(requestID int64, principal executionprincipal.Principal, handle console.SessionHandle) {
	if r.validate() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.backgroundTimeout)
	defer cancel()
	result, err := r.sessions.WaitActive(ctx, principal, handle)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = r.sessions.InterruptActive(context.Background(), principal, handle)
			_ = r.Finish(context.Background(), Completion{ID: requestID, Status: "error", Error: "command timed out while running in background"})
			return
		}
		_ = r.Finish(context.Background(), Completion{ID: requestID, Status: "error", Error: err.Error()})
		return
	}
	status := "completed"
	if result.ExitCode != 0 {
		status = "failed"
	}
	_ = r.Finish(context.Background(), Completion{
		ID: requestID, Status: status, SessionID: result.SessionID,
		Stdout: result.Output, ExitCode: result.ExitCode,
	})
}

func (r *Runtime) CancelRunning(ctx context.Context, errorText string) error {
	if err := r.validate(); err != nil {
		return err
	}
	err := r.store.CancelRunning(ctx, r.projection, r.redact(ctx, errorText))
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
