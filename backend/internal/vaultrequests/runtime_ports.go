package vaultrequests

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	maxIdempotencyKeyBytes = 128
	maxReasonBytes         = 2 << 10
	maxUserNoteBytes       = 8 << 10
	defaultExecutionTTL    = 2 * time.Minute
)

var (
	ErrRuntimeUnavailable  = errors.New("Vault action request runtime is unavailable")
	ErrProjectNotFound     = errors.New("project not found")
	ErrRequestRateLimited  = errors.New("Vault action request rate limit exceeded; retry later")
	ErrMCPExecutionStopped = errors.New("MCP execution is stopped; start MCP before running Vault approvals")
	errMutationUnchanged   = errors.New("Vault action request mutation is unchanged")
)

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type PreparationError struct{ Err error }

func (e PreparationError) Error() string {
	if e.Err == nil {
		return "Vault action request preparation failed"
	}
	return e.Err.Error()
}

func (e PreparationError) Unwrap() error { return e.Err }

type PreparedAction struct {
	ProjectID           int64
	RuntimeID           int64
	Input               map[string]any
	ApprovalContext     ApprovalContext
	ApprovalContextHash string
	RunImmediately      bool
}

type ActionPreparer func(context.Context, int64, string, string, map[string]any) (PreparedAction, error)
type OutputAuthorizer func(context.Context, Request) bool
type RequestLimiter func(int64) bool
type EffectExecutor func(context.Context, Request) (any, error)
type EffectCompensator func(context.Context, Request, any) error
type ProjectionRepairer func(context.Context, int64) error
type ErrorRedactor func(context.Context, error) string
type StaleClassifier func(error) bool

type MutationPort interface {
	WithMutation(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Observe(context.Context, string, *int64, int64, string, any)
}

type RuntimeDependencies struct {
	Store            *Store
	Mutations        MutationPort
	Prepare          ActionPreparer
	AuthorizeOutput  OutputAuthorizer
	AllowRequest     RequestLimiter
	Execute          EffectExecutor
	Compensate       EffectCompensator
	RepairProjection ProjectionRepairer
	RedactError      ErrorRedactor
	IsStale          StaleClassifier
	MCPStarted       func() bool
	ExecutionTimeout time.Duration
}

type Runtime struct {
	store            *Store
	mutations        MutationPort
	prepare          ActionPreparer
	authorizeOutput  OutputAuthorizer
	allowRequest     RequestLimiter
	execute          EffectExecutor
	compensate       EffectCompensator
	repairProjection ProjectionRepairer
	redactError      ErrorRedactor
	isStale          StaleClassifier
	mcpStarted       func() bool
	executionTimeout time.Duration
}

func NewRuntime(dependencies RuntimeDependencies) (*Runtime, error) {
	if dependencies.Store == nil || dependencies.Mutations == nil || dependencies.Prepare == nil ||
		dependencies.AuthorizeOutput == nil || dependencies.AllowRequest == nil || dependencies.Execute == nil ||
		dependencies.Compensate == nil || dependencies.RepairProjection == nil || dependencies.RedactError == nil ||
		dependencies.IsStale == nil || dependencies.MCPStarted == nil {
		return nil, ErrRuntimeUnavailable
	}
	timeout := dependencies.ExecutionTimeout
	if timeout <= 0 {
		timeout = defaultExecutionTTL
	}
	return &Runtime{
		store: dependencies.Store, mutations: dependencies.Mutations,
		prepare: dependencies.Prepare, authorizeOutput: dependencies.AuthorizeOutput,
		allowRequest: dependencies.AllowRequest, execute: dependencies.Execute,
		compensate: dependencies.Compensate, repairProjection: dependencies.RepairProjection,
		redactError: dependencies.RedactError, isStale: dependencies.IsStale,
		mcpStarted: dependencies.MCPStarted, executionTimeout: timeout,
	}, nil
}

func (r *Runtime) validate() error {
	if r == nil || r.store == nil || r.mutations == nil || r.prepare == nil || r.authorizeOutput == nil ||
		r.allowRequest == nil || r.execute == nil || r.compensate == nil || r.repairProjection == nil ||
		r.redactError == nil || r.isStale == nil || r.mcpStarted == nil || r.executionTimeout <= 0 {
		return ErrRuntimeUnavailable
	}
	return nil
}

func (r *Runtime) Validate() error { return r.validate() }
