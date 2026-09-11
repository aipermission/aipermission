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
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

type Dependencies struct {
	MaxJSONBytes    int
	SupportsRunning func(actions.PreparedRequest) bool
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

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

func Delivery(acquire func(context.Context) (func(), error)) actions.DeliveryGate {
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

func (component *Component) Workflow(runtime Workspace) (*actions.Runtime, error) {
	if component == nil || component.dependencies.SupportsRunning == nil || !runtime.workflowReady() {
		return nil, actions.ErrWorkflowUnavailable
	}
	return runtime.Workflow.OrCreate(func() (*actions.Runtime, error) {
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
			Delivery: Delivery(runtime.Workflow.AcquireSecret), MCPStarted: runtime.Identity.MCPStarted,
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
			EnqueueUserNote: func(ctx context.Context, tx *sql.Tx, tokenID int64, message string) error {
				return messagequeue.EnqueueUserNote(ctx, tx, tokenID, message)
			},
		})
		if err != nil {
			return nil, fmt.Errorf("initialize connector action workflow: %w", err)
		}
		return workflow, nil
	})
}

func (component *Component) Call(ctx context.Context, runtime Workspace, call actions.Call) (actions.CallResult, error) {
	workflow, err := component.Workflow(runtime)
	if err != nil {
		return actions.CallResult{}, err
	}
	return workflow.Call(ctx, call)
}

func (component *Component) RunLocal(ctx context.Context, runtime Workspace, call actions.Call) (actions.CallResult, error) {
	workflow, err := component.Workflow(runtime)
	if err != nil {
		return actions.CallResult{}, err
	}
	return workflow.RunLocal(ctx, call)
}

func (component *Component) Finish(ctx context.Context, runtime Workspace, requestID int64, status connectors.ResultStatus, output any, displayText, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	workflow, err := component.Workflow(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return workflow.Finish(ctx, requestID, status, output, displayText, errorText, hints...)
}

func (component *Component) StartRecovery(runtime Workspace) {
	if workflow, err := component.Workflow(runtime); err == nil {
		workflow.StartRecovery()
	}
}

func StopRecovery(runtime Workspace) {
	if runtime.Workflow.Current != nil {
		if workflow := runtime.Workflow.Current(); workflow != nil {
			workflow.StopRecovery()
		}
	}
}

func (component *Component) Redactor(runtime Workspace) (*actions.Redactor, error) {
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

func Prepare(runtime Workspace, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	if runtime.Storage.Database == nil || runtime.Storage.Registry == nil {
		return actions.PreparedRequest{}, errors.New("database runtime is not available")
	}
	return actions.NewService(runtime.Storage.Registry, targetResolver{store: connectortargets.NewStore(runtime.Storage.Database)}).Prepare(ctx, request)
}
