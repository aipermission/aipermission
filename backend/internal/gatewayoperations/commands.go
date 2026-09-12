package gatewayoperations

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/runtimeindex"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

var (
	ErrBulkTargetNotFound        = errors.New("bulk command target not found")
	ErrCommandRuntimeUnavailable = errors.New("command runtime is unavailable")
)

type CommandRuntimeDependencies struct {
	Database          commandrequests.Database
	Vault             *vault.Vault
	WorkspaceID       string
	Redact            func(context.Context, string) string
	Sessions          commandrequests.ActiveSessions
	BackgroundTimeout time.Duration
}

// CommandRuntime is the command owner exposed to gateway composition. The
// commandrequests runtime remains private so lifecycle ownership cannot leak.
type CommandRuntime struct{ owner *commandrequests.Runtime }

type CommandInsert struct {
	TokenID   *int64
	RuntimeID int64
	Source    string
	Command   string
	Reason    string
	Status    string
}

type CommandCompletion struct {
	ID        int64
	Status    string
	SessionID int64
	Stdout    string
	Stderr    string
	ExitCode  int
	Error     string
}

type CommandPolicyWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type CommandRecord struct {
	ID                int64                  `json:"id"`
	TokenID           *int64                 `json:"token_id,omitempty"`
	TokenName         string                 `json:"token_name,omitempty"`
	RuntimeID         int64                  `json:"runtime_id"`
	TargetName        string                 `json:"target_name"`
	Source            string                 `json:"source"`
	Command           string                 `json:"command"`
	Reason            string                 `json:"reason"`
	Status            string                 `json:"status"`
	TrackingReason    string                 `json:"tracking_reason,omitempty"`
	OutputTruncated   bool                   `json:"output_truncated,omitempty"`
	Stdout            string                 `json:"stdout,omitempty"`
	Stderr            string                 `json:"stderr,omitempty"`
	ExitCode          *int                   `json:"exit_code,omitempty"`
	SessionID         *int64                 `json:"session_id,omitempty"`
	UserNote          *string                `json:"user_note,omitempty"`
	Error             string                 `json:"error,omitempty"`
	CreatedAt         string                 `json:"created_at"`
	CompletedAt       *string                `json:"completed_at,omitempty"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                 `json:"assistant_hint,omitempty"`
	PolicyWarnings    []CommandPolicyWarning `json:"policy_warnings,omitempty"`
}

func (runtime *CommandRuntime) Insert(ctx context.Context, request CommandInsert) (int64, error) {
	if runtime == nil || runtime.owner == nil {
		return 0, ErrCommandRuntimeUnavailable
	}
	return runtime.owner.Insert(ctx, commandrequests.Insert{
		TokenID: request.TokenID, RuntimeID: request.RuntimeID, Source: request.Source,
		Command: request.Command, Reason: request.Reason, Status: request.Status,
	})
}

func (runtime *CommandRuntime) Get(ctx context.Context, id, tokenID int64, source string) (CommandRecord, error) {
	if runtime == nil || runtime.owner == nil {
		return CommandRecord{}, ErrCommandRuntimeUnavailable
	}
	record, err := runtime.owner.Get(ctx, id, tokenID, source)
	if err != nil {
		return CommandRecord{}, err
	}
	warnings := make([]CommandPolicyWarning, len(record.PolicyWarnings))
	for index, warning := range record.PolicyWarnings {
		warnings[index] = CommandPolicyWarning{Code: warning.Code, Severity: warning.Severity, Message: warning.Message}
	}
	return CommandRecord{
		ID: record.ID, TokenID: record.TokenID, TokenName: record.TokenName, RuntimeID: record.RuntimeID,
		TargetName: record.TargetName, Source: record.Source, Command: record.Command, Reason: record.Reason,
		Status: record.Status, TrackingReason: record.TrackingReason, OutputTruncated: record.OutputTruncated,
		Stdout: record.Stdout, Stderr: record.Stderr, ExitCode: record.ExitCode, SessionID: record.SessionID,
		UserNote: record.UserNote, Error: record.Error, CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt,
		RetryAfterSeconds: record.RetryAfterSeconds, AssistantHint: record.AssistantHint, PolicyWarnings: warnings,
	}, nil
}

func (runtime *CommandRuntime) ExecutionCommand(ctx context.Context, id int64) (string, error) {
	if runtime == nil || runtime.owner == nil {
		return "", ErrCommandRuntimeUnavailable
	}
	return runtime.owner.ExecutionCommand(ctx, id)
}

func (runtime *CommandRuntime) Finish(ctx context.Context, completion CommandCompletion) error {
	if runtime == nil || runtime.owner == nil {
		return ErrCommandRuntimeUnavailable
	}
	return runtime.owner.Finish(ctx, commandrequests.Completion{
		ID: completion.ID, Status: completion.Status, SessionID: completion.SessionID,
		Stdout: completion.Stdout, Stderr: completion.Stderr, ExitCode: completion.ExitCode, Error: completion.Error,
	})
}

func (runtime *CommandRuntime) CancelRunning(ctx context.Context, errorText string) error {
	if runtime == nil || runtime.owner == nil {
		return ErrCommandRuntimeUnavailable
	}
	return runtime.owner.CancelRunning(ctx, errorText)
}

func (runtime *CommandRuntime) StopWorkers(ctx context.Context) error {
	runtime.BeginWorkerShutdown()
	return runtime.WaitWorkers(ctx)
}

func (runtime *CommandRuntime) BeginWorkerShutdown() {
	if runtime != nil && runtime.owner != nil {
		runtime.owner.BeginWorkerShutdown()
	}
}

func (runtime *CommandRuntime) WaitWorkers(ctx context.Context) error {
	if runtime == nil || runtime.owner == nil {
		return ErrCommandRuntimeUnavailable
	}
	return runtime.owner.WaitWorkers(ctx)
}

func (runtime *CommandRuntime) CancelRunningForSession(ctx context.Context, sessionID int64, errorText string) error {
	if runtime == nil || runtime.owner == nil {
		return ErrCommandRuntimeUnavailable
	}
	return runtime.owner.CancelRunningForSession(ctx, sessionID, errorText)
}

func (runtime *CommandRuntime) CancelRunningForRuntime(ctx context.Context, runtimeID int64, errorText string) (int64, error) {
	if runtime == nil || runtime.owner == nil {
		return 0, ErrCommandRuntimeUnavailable
	}
	return runtime.owner.CancelRunningForRuntime(ctx, runtimeID, errorText)
}

type CommandBulkTarget struct {
	RuntimeID int64
	Name      string
}

type CommandBulkAuditAppender func(*sql.Tx, string, *int64, int64, string, any) error

type CommandBulkHTTPRuntime struct {
	Requests *CommandRuntime
	Sessions interface {
		Exec(context.Context, executionprincipal.Principal, int64, string) (console.ExecResult, error)
	}
	Principal       func() (executionprincipal.Principal, error)
	ResolveTarget   func(context.Context, int64) (CommandBulkTarget, error)
	WithTransaction func(context.Context, func(*sql.Tx, CommandBulkAuditAppender) error) error
	PresentError    func(context.Context, int64, error) string
	InitialTimeout  time.Duration
}

type CommandScopeProviders struct {
	Bulk     func(http.ResponseWriter) (*CommandBulkHTTPRuntime, bool)
	Requests func(http.ResponseWriter) (*CommandRuntime, bool)
}

type CommandHTTPHandlers struct {
	Bulk     *CommandBulkHTTPHandlers
	Requests *CommandRequestHTTPHandlers
}

type CommandBulkHTTPHandlers struct {
	owner *commandrequests.BulkHTTPHandlers
}
type CommandRequestHTTPHandlers struct{ owner *commandrequests.HTTPHandlers }

func (handlers *CommandBulkHTTPHandlers) Run(w http.ResponseWriter, request *http.Request) {
	if handlers == nil || handlers.owner == nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	handlers.owner.Run(w, request)
}

func (handlers *CommandRequestHTTPHandlers) Get(w http.ResponseWriter, request *http.Request) {
	if handlers == nil || handlers.owner == nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	handlers.owner.Get(w, request)
}

type CommandComponent struct {
	runtimes runtimeindex.Index[*CommandRuntime]
}

func (component *CommandComponent) HTTPHandlers(providers CommandScopeProviders) CommandHTTPHandlers {
	if component == nil {
		return CommandHTTPHandlers{}
	}
	bulk := commandrequests.NewBulkHTTPHandlers(func(w http.ResponseWriter) (*commandrequests.BulkHTTPRuntime, bool) {
		if providers.Bulk == nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return nil, false
		}
		runtime, ok := providers.Bulk(w)
		if !ok || runtime == nil {
			return nil, ok
		}
		if runtime.Requests == nil || runtime.Requests.owner == nil || runtime.Sessions == nil ||
			runtime.Principal == nil || runtime.ResolveTarget == nil || runtime.WithTransaction == nil {
			return nil, true
		}
		return &commandrequests.BulkHTTPRuntime{
			Requests: runtime.Requests.owner, Sessions: runtime.Sessions, Principal: runtime.Principal,
			ResolveTarget: func(ctx context.Context, runtimeID int64) (commandrequests.BulkTarget, error) {
				target, err := runtime.ResolveTarget(ctx, runtimeID)
				if errors.Is(err, ErrBulkTargetNotFound) {
					return commandrequests.BulkTarget{}, commandrequests.ErrBulkTargetNotFound
				}
				return commandrequests.BulkTarget{RuntimeID: target.RuntimeID, Name: target.Name}, err
			},
			WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, commandrequests.BulkAuditAppender) error) error {
				return runtime.WithTransaction(ctx, func(tx *sql.Tx, appendAudit CommandBulkAuditAppender) error {
					return mutate(tx, commandrequests.BulkAuditAppender(appendAudit))
				})
			},
			PresentError: runtime.PresentError, InitialTimeout: runtime.InitialTimeout,
		}, true
	})
	requests := commandrequests.NewHTTPHandlers(func(w http.ResponseWriter) (commandrequests.HTTPReader, bool) {
		if providers.Requests == nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return nil, false
		}
		runtime, ok := providers.Requests(w)
		if !ok || runtime == nil || runtime.owner == nil {
			return nil, ok
		}
		return runtime.owner, true
	})
	return CommandHTTPHandlers{
		Bulk: &CommandBulkHTTPHandlers{owner: bulk}, Requests: &CommandRequestHTTPHandlers{owner: requests},
	}
}

func (component *CommandComponent) Initialize(runtimeID string, dependencies CommandRuntimeDependencies) error {
	if component == nil {
		return ErrCommandRuntimeUnavailable
	}
	_, err := component.runtimes.LoadOrCreate(runtimeID, func() (*CommandRuntime, error) {
		owner, err := commandrequests.NewWorkspaceRuntime(commandrequests.WorkspaceRuntimeDependencies{
			Database: dependencies.Database, Vault: dependencies.Vault, WorkspaceID: dependencies.WorkspaceID,
			Redact: dependencies.Redact, Sessions: dependencies.Sessions, BackgroundTimeout: dependencies.BackgroundTimeout,
		})
		if err != nil {
			return nil, ErrCommandRuntimeUnavailable
		}
		return &CommandRuntime{owner: owner}, nil
	})
	if err != nil {
		return ErrCommandRuntimeUnavailable
	}
	return nil
}

func (component *CommandComponent) Runtime(runtimeID string) (*CommandRuntime, error) {
	if component == nil {
		return nil, ErrCommandRuntimeUnavailable
	}
	runtime, ok := component.runtimes.Load(runtimeID)
	if !ok || runtime == nil || runtime.owner == nil {
		return nil, ErrCommandRuntimeUnavailable
	}
	return runtime, nil
}

func (component *CommandComponent) Release(runtimeID string) {
	if component != nil {
		component.runtimes.Delete(runtimeID)
	}
}
