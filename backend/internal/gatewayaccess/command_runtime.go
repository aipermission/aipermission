package gatewayaccess

import (
	"context"
	"time"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

var commandRuntimeStateKey = componentstate.NewKey[*commandRuntimeHandle]("command-request-runtime")

type CommandSessions interface {
	WaitActive(context.Context, executionprincipal.Principal, console.SessionHandle) (console.ExecResult, error)
	InterruptActive(context.Context, executionprincipal.Principal, console.SessionHandle) error
}

type CommandRuntimeDependencies struct {
	Database          commandrequests.Database
	Vault             *vault.Vault
	WorkspaceID       string
	Redact            commandrequests.Redactor
	Sessions          CommandSessions
	BackgroundTimeout time.Duration
}

type CommandRuntime interface {
	Prepare(context.Context, commandrequests.Insert) (commandrequests.PreparedInsert, error)
	Insert(context.Context, commandrequests.Insert) (int64, error)
	InsertPrepared(context.Context, commandrequests.Executor, commandrequests.PreparedInsert) (int64, error)
	ExecutionCommand(context.Context, int64) (string, error)
	Get(context.Context, int64, int64, string) (commandrequests.Record, error)
	SetSession(context.Context, int64, int64) error
	Finish(context.Context, commandrequests.Completion) error
	FinishActive(int64, executionprincipal.Principal, console.SessionHandle)
	CancelRunning(context.Context, string) error
	CancelRunningForSession(context.Context, int64, string) error
	CancelRunningForRuntime(context.Context, int64, string) (int64, error)
}

type commandRuntimeHandle struct{ runtime CommandRuntime }

func (component *Component) InitializeCommandRuntime(state componentstate.Port, dependencies CommandRuntimeDependencies) error {
	if component == nil {
		return ErrComponentUnavailable
	}
	_, err := componentstate.LoadOrCreate(state, commandRuntimeStateKey, func() (*commandRuntimeHandle, error) {
		runtime, err := commandrequests.NewWorkspaceRuntime(commandrequests.WorkspaceRuntimeDependencies{
			Database: dependencies.Database, Vault: dependencies.Vault, WorkspaceID: dependencies.WorkspaceID,
			Redact: dependencies.Redact, Sessions: dependencies.Sessions, BackgroundTimeout: dependencies.BackgroundTimeout,
		})
		if err != nil {
			return nil, err
		}
		return &commandRuntimeHandle{runtime: runtime}, nil
	})
	return err
}

func (component *Component) CommandRuntime(state componentstate.Port) (CommandRuntime, error) {
	if component == nil {
		return nil, ErrComponentUnavailable
	}
	handle, ok, err := componentstate.Load[*commandRuntimeHandle](state, commandRuntimeStateKey)
	if err != nil {
		return nil, err
	}
	if !ok || handle == nil || handle.runtime == nil {
		return nil, ErrCommandRuntimeUnavailable
	}
	return handle.runtime, nil
}
