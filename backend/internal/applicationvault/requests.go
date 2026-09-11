package applicationvault

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type RequestDependencies struct {
	Store            func(context.Context, workspaceruntime.Port) *vaultrequests.Store
	Mutate           func(context.Context, workspaceruntime.Port, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Observe          func(context.Context, workspaceruntime.Port, string, *int64, int64, string, any)
	AllowRequest     func(workspaceruntime.Port, int64) bool
	RepairProjection func(context.Context, workspaceruntime.Port, int64) error
	RedactError      func(context.Context, workspaceruntime.Port, error) string
}

func (component *Component) ConfigureRequests(dependencies RequestDependencies) {
	component.requests = dependencies
}

type requestMutationPort struct {
	component *Component
	runtime   workspaceruntime.Port
}

func (port requestMutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime == nil || port.component.requests.Mutate == nil {
		return vaultrequests.ErrRuntimeUnavailable
	}
	return port.component.requests.Mutate(ctx, port.runtime, actor, tokenID, runtimeID, action, payload, mutate)
}

func (port requestMutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if port.component != nil && port.runtime != nil && port.component.requests.Observe != nil {
		port.component.requests.Observe(ctx, port.runtime, actor, tokenID, runtimeID, action, payload)
	}
}

func (component *Component) RequestRuntime(ctx context.Context, runtime workspaceruntime.Port) (*vaultrequests.Runtime, error) {
	if component == nil || runtime == nil || runtime.StoragePort().DatabaseHandle() == nil || component.requests.Store == nil ||
		component.requests.AllowRequest == nil || component.requests.RepairProjection == nil || component.requests.RedactError == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	actions, err := component.ActionRuntime(runtime)
	if err != nil {
		return nil, err
	}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store: component.requests.Store(ctx, runtime), Mutations: requestMutationPort{component: component, runtime: runtime},
		Prepare: actions.Prepare, AuthorizeOutput: actions.AuthorizeOutput,
		AllowRequest: func(tokenID int64) bool { return component.requests.AllowRequest(runtime, tokenID) },
		Execute:      actions.Execute, Compensate: actions.Compensate,
		RepairProjection: func(ctx context.Context, id int64) error {
			return component.requests.RepairProjection(ctx, runtime, id)
		},
		RedactError: func(ctx context.Context, err error) string { return component.requests.RedactError(ctx, runtime, err) },
		IsStale:     actions.IsStale, MCPStarted: runtime.IsMCPStarted,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}
