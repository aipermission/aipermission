// Package applicationconnectoractions composes connector action workflows
// around an active encrypted workspace runtime.
package applicationconnectoractions

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
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type Dependencies struct {
	MaxJSONBytes    int
	EnsureIdentity  func(*workspaceruntime.Runtime) error
	RedactBasic     func(context.Context, *workspaceruntime.Runtime, string) string
	RedactCustom    func(context.Context, *workspaceruntime.Runtime, string) string
	Mutate          func(context.Context, *workspaceruntime.Runtime, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Transaction     func(context.Context, *workspaceruntime.Runtime, func(*sql.Tx, actions.AuditAppender) error) error
	Observe         func(context.Context, *workspaceruntime.Runtime, string, *int64, int64, string, any)
	Capabilities    func(string, *workspaceruntime.Runtime, []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver
	SupportsRunning func(actions.PreparedRequest) bool
	FinishRunning   func(*workspaceruntime.Runtime, int64, actions.PreparedRequest, executionprincipal.Principal, connectors.ActionHandles)
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

type tokenReader struct{ runtime *workspaceruntime.Runtime }

func (reader tokenReader) Get(ctx context.Context, id int64, now time.Time) (actions.AuthorizationToken, error) {
	if reader.runtime == nil || reader.runtime.Storage.Tokens == nil {
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

type deliveryGate struct{ runtime *workspaceruntime.Runtime }

func Delivery(runtime *workspaceruntime.Runtime) actions.DeliveryGate {
	return deliveryGate{runtime: runtime}
}

func (gate deliveryGate) Acquire(ctx context.Context) (func(), error) {
	if gate.runtime == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return gate.runtime.Security.VaultDelivery.AcquireDelivery(ctx)
}

type sealedRecords struct{ runtime *workspaceruntime.Runtime }

func (records sealedRecords) SealActionRequest(id int64, envelope actions.ExecutionEnvelope) (string, error) {
	if records.runtime == nil || records.runtime.Storage.Vault == nil {
		return "", actions.ErrWorkflowUnavailable
	}
	return recordcrypto.EncryptJSON(records.runtime.Storage.Vault, records.runtime.WorkspaceUUID, recordcrypto.ConnectorActionRequest, id, envelope)
}
func (records sealedRecords) OpenActionRequest(id int64, sealed string) (actions.ExecutionEnvelope, error) {
	if records.runtime == nil || records.runtime.Storage.Vault == nil {
		return actions.ExecutionEnvelope{}, actions.ErrWorkflowUnavailable
	}
	var envelope actions.ExecutionEnvelope
	err := recordcrypto.DecryptJSON(records.runtime.Storage.Vault, records.runtime.WorkspaceUUID, recordcrypto.ConnectorActionRequest, id, sealed, &envelope)
	return envelope, err
}
func (records sealedRecords) OpenCredentialProfile(id int64, sealed string) (map[string]any, error) {
	secrets := map[string]any{}
	if sealed == "" {
		return secrets, nil
	}
	if records.runtime == nil || records.runtime.Storage.Vault == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	err := recordcrypto.DecryptJSON(records.runtime.Storage.Vault, records.runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, id, sealed, &secrets)
	return secrets, err
}

type mutationPort struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (port mutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	return port.component.dependencies.Mutate(ctx, port.runtime, actor, tokenID, runtimeID, action, payload, mutate)
}
func (port mutationPort) WithTransaction(ctx context.Context, mutate func(*sql.Tx, actions.AuditAppender) error) error {
	return port.component.dependencies.Transaction(ctx, port.runtime, mutate)
}
func (port mutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	port.component.dependencies.Observe(ctx, port.runtime, actor, tokenID, runtimeID, action, payload)
}

type runningPort struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (port runningPort) SupportsRunning(prepared actions.PreparedRequest) bool {
	return port.component.dependencies.SupportsRunning(prepared)
}
func (port runningPort) FinishRunning(id int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
	port.component.dependencies.FinishRunning(port.runtime, id, prepared, principal, handles)
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

func (component *Component) Workflow(runtime *workspaceruntime.Runtime) (*actions.Runtime, error) {
	if component == nil || runtime == nil {
		return nil, actions.ErrWorkflowUnavailable
	}
	return runtime.Operations.ActionWorkflowOrCreate(func() (*actions.Runtime, error) {
		redactor, err := actions.NewRedactor(
			func(ctx context.Context, value string) string {
				return component.dependencies.RedactBasic(ctx, runtime, value)
			},
			func(ctx context.Context, value string) string {
				return component.dependencies.RedactCustom(ctx, runtime, value)
			}, component.dependencies.MaxJSONBytes,
		)
		if err != nil {
			return nil, err
		}
		workflow, err := actions.NewRuntime(actions.RuntimeDependencies{
			Database: runtime.Storage.Database, Tokens: tokenReader{runtime: runtime}, Registry: runtime.Connectors.ConnectorRegistry(),
			Targets: targetResolver{store: connectortargets.NewStore(runtime.Storage.Database)}, IdentityKey: runtime.ActionIdentityKey,
			Delivery: deliveryGate{runtime: runtime}, MCPStarted: runtime.IsMCPStarted,
			Identity: func() (string, string, error) {
				if err := component.dependencies.EnsureIdentity(runtime); err != nil {
					return "", "", err
				}
				return runtime.WorkspaceUUID, runtime.RuntimeInstanceID, nil
			},
			Redactor: redactor, SealedRecords: sealedRecords{runtime: runtime}, Mutations: mutationPort{component: component, runtime: runtime},
			Capabilities: func(kind string, dependencies []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver {
				return component.dependencies.Capabilities(kind, runtime, dependencies)
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

func Prepare(runtime *workspaceruntime.Runtime, ctx context.Context, request actions.PrepareRequest) (actions.PreparedRequest, error) {
	if runtime == nil || runtime.Storage.Database == nil {
		return actions.PreparedRequest{}, errors.New("database runtime is not available")
	}
	return actions.NewService(runtime.Connectors.ConnectorRegistry(), targetResolver{store: connectortargets.NewStore(runtime.Storage.Database)}).Prepare(ctx, request)
}
