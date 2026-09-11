package gatewayconnectoractions

import (
	"context"
	"database/sql"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

// Workspace is the action-owner view of one unlocked workspace.
type Workspace struct {
	Storage  ActionStorage
	Identity ActionIdentity
	Workflow WorkflowPorts
}

type ActionStorage struct {
	Database    *sql.DB
	Tokens      *tokens.Store
	Registry    *connectors.Registry
	SecretVault *vault.Vault
	WorkspaceID string
}

type ActionIdentity struct {
	Tag               func([]byte) (string, error)
	RuntimeInstanceID string
	MCPStarted        func() bool
	Ensure            func() error
}

type WorkflowPorts struct {
	AcquireSecret func(context.Context) (func(), error)
	RedactBasic   func(context.Context, string) string
	RedactCustom  func(context.Context, string) string
	Mutate        func(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Transaction   func(context.Context, func(*sql.Tx, AuditAppender) error) error
	Observe       func(context.Context, string, *int64, int64, string, any)
	Capabilities  func(string, []connectors.ResolvedDependency) connectors.RuntimeCapabilityResolver
	FinishRunning func(context.Context, int64, PreparedRequest, executionprincipal.Principal, connectors.ActionHandles)
}

type AuditAppender func(*sql.Tx, string, *int64, int64, string, any) error

type PrepareRequest struct {
	Source     string
	TargetRef  string
	ActionName string
	Input      map[string]any
	Reason     string
	CreatedAt  time.Time
}

func (request PrepareRequest) domain() actions.PrepareRequest {
	return actions.PrepareRequest{
		Source: request.Source, TargetRef: request.TargetRef, ActionName: request.ActionName,
		Input: request.Input, Reason: request.Reason, CreatedAt: request.CreatedAt,
	}
}

type PreparedRequest struct {
	Target           connectors.TargetView
	Profile          connectors.CredentialProfileView
	ConnectorVersion string
	ActionDefinition connectors.ActionDefinition
	Action           connectors.PreparedAction
	Requested        PrepareRequest
	IdempotencyInput map[string]any
	Dependencies     []connectors.ResolvedDependency
	value            actions.PreparedRequest
}

func wrapPreparedRequest(value actions.PreparedRequest) PreparedRequest {
	return PreparedRequest{
		Target: value.Target, Profile: value.Profile, ConnectorVersion: value.ConnectorVersion,
		ActionDefinition: value.ActionDefinition, Action: value.Action,
		Requested: PrepareRequest{
			Source: value.Requested.Source, TargetRef: value.Requested.TargetRef, ActionName: value.Requested.ActionName,
			Input: value.Requested.Input, Reason: value.Requested.Reason, CreatedAt: value.Requested.CreatedAt,
		},
		IdempotencyInput: value.IdempotencyInput, Dependencies: value.Dependencies, value: value,
	}
}

func (prepared PreparedRequest) domain() actions.PreparedRequest {
	return actions.PreparedRequest{
		Target: prepared.Target, Profile: prepared.Profile, ConnectorVersion: prepared.ConnectorVersion,
		ActionDefinition: prepared.ActionDefinition, Action: prepared.Action, Requested: prepared.Requested.domain(),
		IdempotencyInput: prepared.IdempotencyInput, Dependencies: prepared.Dependencies,
	}
}
func (prepared PreparedRequest) Adapter() connectors.RuntimeActionContext {
	return connectors.RuntimeActionContext{
		TargetConnectorKind: prepared.Target.ConnectorKind,
		ActionName:          prepared.Action.ActionName, TargetRef: prepared.Action.TargetRef,
		OutputHint: prepared.ActionDefinition.OutputHint,
	}
}

type ExecutionOptions struct {
	Permission              connectortargets.ActionPermission
	RequiredPermissionRule  connectortargets.ActionPermissionRule
	UnsupportedRunningError string
	ApprovalPendingError    string
	FollowupTool            string
}

func (options ExecutionOptions) domain() actions.ExecutionOptions {
	return actions.ExecutionOptions{
		Permission: options.Permission, RequiredPermissionRule: options.RequiredPermissionRule,
		UnsupportedRunningError: options.UnsupportedRunningError, ApprovalPendingError: options.ApprovalPendingError,
		FollowupTool: options.FollowupTool,
	}
}

type CredentialBoundaryPort interface {
	Add(...string)
	AddStructured(any)
	Empty() bool
	Redact(string) string
	RedactKey(string) string
	RedactStructured(any) any
	Valid() bool
}

type CredentialBoundary struct {
	value    CredentialBoundaryPort
	tracking actions.CredentialBoundary
}

func newCredentialBoundary(value actions.CredentialBoundary) CredentialBoundary {
	return CredentialBoundary{value: value, tracking: value}
}

func AdoptCredentialBoundary(value CredentialBoundaryPort) CredentialBoundary {
	return CredentialBoundary{value: value}
}

func (boundary CredentialBoundary) effective() CredentialBoundaryPort {
	if boundary.value == nil {
		return actions.CredentialBoundary{}
	}
	return boundary.value
}

func (boundary CredentialBoundary) Add(values ...string) { boundary.effective().Add(values...) }
func (boundary CredentialBoundary) AddStructured(value any) {
	boundary.effective().AddStructured(value)
}
func (boundary CredentialBoundary) Empty() bool { return boundary.effective().Empty() }
func (boundary CredentialBoundary) Redact(value string) string {
	return boundary.effective().Redact(value)
}
func (boundary CredentialBoundary) RedactKey(value string) string {
	return boundary.effective().RedactKey(value)
}
func (boundary CredentialBoundary) RedactStructured(value any) any {
	return boundary.effective().RedactStructured(value)
}
func (boundary CredentialBoundary) Valid() bool { return boundary.effective().Valid() }

type ExecutionSnapshot struct {
	Secrets            map[string]any
	CredentialBoundary CredentialBoundary
}

type Call struct {
	Source         string
	TokenID        int64
	TargetRef      string
	ActionName     string
	Input          map[string]any
	Reason         string
	IdempotencyKey string
}

func (call Call) domain() actions.Call {
	return actions.Call{
		Source: call.Source, TokenID: call.TokenID, TargetRef: call.TargetRef,
		ActionName: call.ActionName, Input: call.Input, Reason: call.Reason,
		IdempotencyKey: call.IdempotencyKey,
	}
}

type CallResult struct {
	Request    connectortargets.ActionRequest
	Permission connectortargets.ActionPermission
	Result     connectors.ActionResult
	Replayed   bool
}

type Response struct {
	Status            string                 `json:"status"`
	RequestID         int64                  `json:"request_id,omitempty"`
	TargetRef         string                 `json:"target_ref"`
	TargetName        string                 `json:"target_name,omitempty"`
	ConnectorKind     string                 `json:"connector_kind"`
	ProfileLabel      string                 `json:"profile_label,omitempty"`
	ActionName        string                 `json:"action_name"`
	Input             map[string]any         `json:"input,omitempty"`
	Output            any                    `json:"output,omitempty"`
	DisplayText       string                 `json:"display_text,omitempty"`
	Error             string                 `json:"error,omitempty"`
	RetryPolicy       connectors.RetryPolicy `json:"retry_policy"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	AssistantHint     string                 `json:"assistant_hint,omitempty"`
	OutputWithheld    bool                   `json:"output_withheld,omitempty"`
	Replayed          bool                   `json:"replayed,omitempty"`
}

func wrapResponse(response actions.Response) Response {
	return Response{
		Status: response.Status, RequestID: response.RequestID, TargetRef: response.TargetRef,
		TargetName: response.TargetName, ConnectorKind: response.ConnectorKind, ProfileLabel: response.ProfileLabel,
		ActionName: response.ActionName, Input: response.Input, Output: response.Output, DisplayText: response.DisplayText,
		Error: response.Error, RetryPolicy: response.RetryPolicy, RetryAfterSeconds: response.RetryAfterSeconds,
		AssistantHint: response.AssistantHint, OutputWithheld: response.OutputWithheld, Replayed: response.Replayed,
	}
}

func wrapCallResult(result actions.CallResult) CallResult {
	return CallResult{Request: result.Request, Permission: result.Permission, Result: result.Result, Replayed: result.Replayed}
}

func (workspace Workspace) workflowReady() bool {
	return workspace.Storage.Database != nil && workspace.Storage.Tokens != nil && workspace.Storage.Registry != nil &&
		workspace.Storage.SecretVault != nil && workspace.Identity.RuntimeInstanceID != "" && workspace.Identity.MCPStarted != nil && workspace.Identity.Ensure != nil &&
		workspace.Identity.Tag != nil &&
		workspace.Workflow.AcquireSecret != nil && workspace.Workflow.RedactBasic != nil &&
		workspace.Workflow.RedactCustom != nil && workspace.Workflow.Mutate != nil && workspace.Workflow.Transaction != nil &&
		workspace.Workflow.Observe != nil && workspace.Workflow.Capabilities != nil && workspace.Workflow.FinishRunning != nil
}
