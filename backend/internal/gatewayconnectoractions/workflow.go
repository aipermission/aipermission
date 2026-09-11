// Package gatewayconnectoractions composes connector action workflows
// around an active encrypted workspace runtime.
package gatewayconnectoractions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

var workflowStateKey = componentstate.NewKey[*workflowHandle]("connector-action-workflow")

type Dependencies struct {
	MaxJSONBytes    int
	SupportsRunning func(actions.PreparedRequest) bool
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

type workflowHandle struct{ runtime *actions.Runtime }

type ApprovalWorkflow interface {
	ApprovalPreview(context.Context, connectortargets.ActionRequest) (map[string]any, error)
	RunPending(context.Context, int64, string) (connectortargets.ActionRequest, error)
	DeclinePending(context.Context, int64, string) (connectortargets.ActionRequest, error)
}

type ShutdownWorkflow interface {
	StopRecovery()
	MarkRunningOutcomeUnknown(context.Context, string) error
}

func (workflow *workflowHandle) ApprovalPreview(ctx context.Context, request connectortargets.ActionRequest) (map[string]any, error) {
	return workflow.runtime.ApprovalPreview(ctx, request)
}

func (workflow *workflowHandle) RunPending(ctx context.Context, id int64, note string) (connectortargets.ActionRequest, error) {
	return workflow.runtime.RunPending(ctx, id, note)
}

func (workflow *workflowHandle) DeclinePending(ctx context.Context, id int64, note string) (connectortargets.ActionRequest, error) {
	return workflow.runtime.DeclinePending(ctx, id, note)
}

func (workflow *workflowHandle) StopRecovery() { workflow.runtime.StopRecovery() }

func (workflow *workflowHandle) MarkRunningOutcomeUnknown(ctx context.Context, reason string) error {
	return workflow.runtime.MarkRunningOutcomeUnknown(ctx, reason)
}

type SecretAccessor struct {
	Values   map[string]any
	Boundary actions.CredentialBoundary
}

func (accessor SecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := accessor.Values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	accessor.Boundary.Add(text)
	return text, nil
}

func (accessor SecretAccessor) RegisterSensitiveValue(value string) { accessor.Boundary.Add(value) }

type NoopEventSink struct{}

func (NoopEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

type tokenReader struct{ runtime Workspace }

func (reader tokenReader) Get(ctx context.Context, id int64, now time.Time) (actions.AuthorizationToken, error) {
	if reader.runtime.Storage.Tokens == nil {
		return actions.AuthorizationToken{}, actions.ErrWorkflowUnavailable
	}
	token, err := reader.runtime.Storage.Tokens.Get(ctx, id)
	if errors.Is(err, tokens.ErrNotFound) {
		return actions.AuthorizationToken{}, actions.ErrTokenNotFound
	}
	if err != nil {
		return actions.AuthorizationToken{}, err
	}
	return actions.AuthorizationToken{ID: token.ID, RevokedAt: token.RevokedAt, ExpiresAt: token.ExpiresAt, Active: tokens.Active(token.RevokedAt, token.ExpiresAt, now)}, nil
}

type deliveryGate struct {
	acquire func(context.Context) (func(), error)
}

func (*Component) Delivery(acquire func(context.Context) (func(), error)) actions.DeliveryGate {
	return deliveryGate{acquire: acquire}
}

func (gate deliveryGate) Acquire(ctx context.Context) (func(), error) {
	if gate.acquire == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return gate.acquire(ctx)
}

type sealedRecords struct{ runtime Workspace }

func (records sealedRecords) SealActionRequest(id int64, envelope actions.ExecutionEnvelope) (string, error) {
	if records.runtime.Storage.SecretVault == nil {
		return "", actions.ErrWorkflowUnavailable
	}
	return recordcrypto.EncryptJSON(records.runtime.Storage.SecretVault, records.runtime.Storage.WorkspaceID, recordcrypto.ConnectorActionRequest, id, envelope)
}
func (records sealedRecords) OpenActionRequest(id int64, sealed string) (actions.ExecutionEnvelope, error) {
	if records.runtime.Storage.SecretVault == nil {
		return actions.ExecutionEnvelope{}, actions.ErrWorkflowUnavailable
	}
	var envelope actions.ExecutionEnvelope
	err := recordcrypto.DecryptJSON(records.runtime.Storage.SecretVault, records.runtime.Storage.WorkspaceID, recordcrypto.ConnectorActionRequest, id, sealed, &envelope)
	return envelope, err
}
func (records sealedRecords) OpenCredentialProfile(id int64, sealed string) (map[string]any, error) {
	secrets := map[string]any{}
	if sealed == "" {
		return secrets, nil
	}
	if records.runtime.Storage.SecretVault == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	err := recordcrypto.DecryptJSON(records.runtime.Storage.SecretVault, records.runtime.Storage.WorkspaceID, recordcrypto.ConnectorCredentialProfile, id, sealed, &secrets)
	return secrets, err
}

type mutationPort struct {
	component *Component
	runtime   Workspace
}

func (port mutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	return port.runtime.Workflow.Mutate(ctx, actor, tokenID, runtimeID, action, payload, mutate)
}
func (port mutationPort) WithTransaction(ctx context.Context, mutate func(*sql.Tx, actions.AuditAppender) error) error {
	return port.runtime.Workflow.Transaction(ctx, mutate)
}
func (port mutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	port.runtime.Workflow.Observe(ctx, actor, tokenID, runtimeID, action, payload)
}

type runningPort struct {
	component *Component
	runtime   Workspace
}

func (port runningPort) SupportsRunning(prepared actions.PreparedRequest) bool {
	return port.component.dependencies.SupportsRunning(prepared)
}
func (port runningPort) FinishRunning(id int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
	port.runtime.Workflow.FinishRunning(id, prepared, principal, handles)
}

type targetResolver struct{ store *connectortargets.Store }

func (resolver targetResolver) ResolveActionTarget(ctx context.Context, ref string) (actions.ResolvedTarget, error) {
	target, profile, err := resolver.store.ResolveConnectorActionTarget(ctx, ref)
	if err == nil {
		return actions.ResolvedTarget{Target: target, Profile: profile}, nil
	}
	if errors.Is(err, connectortargets.ErrInvalidTargetRef) || errors.Is(err, connectortargets.ErrTargetNotFound) || errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		return actions.ResolvedTarget{}, actions.ErrTargetNotFound
	}
	return actions.ResolvedTarget{}, err
}

func (component *Component) workflow(runtime Workspace) (*workflowHandle, error) {
	if component == nil || component.dependencies.SupportsRunning == nil || !runtime.workflowReady() {
		return nil, actions.ErrWorkflowUnavailable
	}
	workflow, err := componentstate.LoadOrCreate(runtime.Workflow.State, workflowStateKey, func() (*workflowHandle, error) {
		redactor, err := actions.NewRedactor(
			func(ctx context.Context, value string) string {
				return runtime.Workflow.RedactBasic(ctx, value)
			},
			func(ctx context.Context, value string) string {
				return runtime.Workflow.RedactCustom(ctx, value)
			}, component.dependencies.MaxJSONBytes,
		)
		if err != nil {
			return nil, err
		}
		workflow, err := actions.NewRuntime(actions.RuntimeDependencies{
			Database: runtime.Storage.Database, Tokens: tokenReader{runtime: runtime}, Registry: runtime.Storage.Registry,
			Targets: targetResolver{store: connectortargets.NewStore(runtime.Storage.Database)}, IdentityKey: runtime.Identity.Key,
			Delivery: component.Delivery(runtime.Workflow.AcquireSecret), MCPStarted: runtime.Identity.MCPStarted,
			Identity: func() (string, string, error) {
				if runtime.Identity.Ensure == nil {
					return "", "", actions.ErrWorkflowUnavailable
				}
				if err := runtime.Identity.Ensure(); err != nil {
					return "", "", err
				}
				return runtime.Storage.WorkspaceID, runtime.Identity.RuntimeInstanceID, nil
			},
			Redactor: redactor, SealedRecords: sealedRecords{runtime: runtime}, Mutations: mutationPort{component: component, runtime: runtime},
			Capabilities: func(kind string, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
				return runtime.Workflow.Capabilities(kind, dependencies)
			},
			RunningActions: runningPort{component: component, runtime: runtime},
		})
		if err != nil {
			return nil, fmt.Errorf("initialize connector action workflow: %w", err)
		}
		return &workflowHandle{runtime: workflow}, nil
	})
	if err != nil {
		return nil, err
	}
	if workflow == nil || workflow.runtime == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return workflow, nil
}

func (component *Component) Approval(runtime Workspace) (ApprovalWorkflow, error) {
	return component.workflow(runtime)
}

func (component *Component) Shutdown(runtime Workspace) (ShutdownWorkflow, error) {
	return component.workflow(runtime)
}

func (component *Component) Call(ctx context.Context, runtime Workspace, call actions.Call) (actions.CallResult, error) {
	workflow, err := component.workflow(runtime)
	if err != nil {
		return actions.CallResult{}, err
	}
	return workflow.runtime.Call(ctx, call)
}

func (component *Component) RunLocal(ctx context.Context, runtime Workspace, call actions.Call) (actions.CallResult, error) {
	workflow, err := component.workflow(runtime)
	if err != nil {
		return actions.CallResult{}, err
	}
	return workflow.runtime.RunLocal(ctx, call)
}

func (component *Component) Finish(ctx context.Context, runtime Workspace, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	workflow, err := component.workflow(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return workflow.runtime.Finish(ctx, requestID, status, output, displayText, errorText, hints...)
}

func (component *Component) StartRecovery(runtime Workspace) {
	if workflow, err := component.workflow(runtime); err == nil {
		workflow.runtime.StartRecovery()
	}
}

func (component *Component) StopRecovery(runtime Workspace) {
	if runtime.Workflow.State == nil {
		return
	}
	workflow, ok, err := componentstate.Load[*workflowHandle](runtime.Workflow.State, workflowStateKey)
	if err == nil && ok && workflow != nil && workflow.runtime != nil {
		workflow.runtime.StopRecovery()
	}
}

func (component *Component) redactor(runtime Workspace) (*actions.Redactor, error) {
	if component == nil || runtime.Workflow.RedactBasic == nil || runtime.Workflow.RedactCustom == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return actions.NewRedactor(
		func(ctx context.Context, value string) string {
			return runtime.Workflow.RedactBasic(ctx, value)
		},
		func(ctx context.Context, value string) string {
			return runtime.Workflow.RedactCustom(ctx, value)
		},
		component.dependencies.MaxJSONBytes,
	)
}

func (component *Component) RedactValue(ctx context.Context, runtime Workspace, value any, sensitiveFields, capabilityFields map[string]bool, boundary actions.CredentialBoundary) (any, error) {
	redactor, err := component.redactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.ValueWithCredentialBoundary(ctx, value, sensitiveFields, capabilityFields, boundary)
}

func (component *Component) RedactResult(ctx context.Context, runtime Workspace, result connectors.ActionResult, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	redactor, err := component.redactor(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return redactor.Result(ctx, result, hints...)
}

func (component *Component) RedactResultWithCredentialBoundary(ctx context.Context, runtime Workspace, result connectors.ActionResult, boundary actions.CredentialBoundary, hints ...connectors.OutputHint) (connectors.ActionResult, error) {
	redactor, err := component.redactor(runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return redactor.ResultWithCredentialBoundary(ctx, result, boundary, hints...)
}

func (component *Component) RedactInput(ctx context.Context, runtime Workspace, input map[string]any, sensitiveFields []string) (map[string]any, error) {
	redactor, err := component.redactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.Input(ctx, input, sensitiveFields)
}

func (component *Component) RedactPreview(ctx context.Context, runtime Workspace, preview map[string]any, sensitiveFields []string, hints ...connectors.OutputHint) (map[string]any, error) {
	redactor, err := component.redactor(runtime)
	if err != nil {
		return nil, err
	}
	return redactor.Preview(ctx, preview, sensitiveFields, hints...)
}

func (*Component) Prepare(runtime Workspace, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	if runtime.Storage.Database == nil || runtime.Storage.Registry == nil {
		return actions.PreparedRequest{}, errors.New("database runtime is not available")
	}
	return actions.NewService(runtime.Storage.Registry, targetResolver{store: connectortargets.NewStore(runtime.Storage.Database)}).Prepare(ctx, request)
}
