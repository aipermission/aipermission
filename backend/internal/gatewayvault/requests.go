package gatewayvault

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

type requestMutationPort struct {
	component *Component
	runtime   Runtime
}

func (port requestMutationPort) WithMutation(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime.Requests.Mutate == nil {
		return vaultrequests.ErrRuntimeUnavailable
	}
	return port.runtime.Requests.Mutate(ctx, actor, tokenID, runtimeID, action, payload, mutate)
}

func (port requestMutationPort) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	if port.component != nil && port.runtime.Requests.Observe != nil {
		port.runtime.Requests.Observe(ctx, actor, tokenID, runtimeID, action, payload)
	}
}

func (component *Component) RequestRuntime(ctx context.Context, runtime Runtime) (VaultRequestApplication, error) {
	if component == nil || runtime.Storage.Database == nil || runtime.Storage.DatabaseID == "" || runtime.Requests.Store == nil || component.dependencies.AllowRequest == nil ||
		runtime.Requests.RepairProjection == nil || runtime.Requests.RedactRequestError == nil || runtime.Session.MCPStarted == nil {
		return nil, vaultrequests.ErrRuntimeUnavailable
	}
	actions, err := component.ActionRuntime(runtime)
	if err != nil {
		return nil, err
	}
	owner, err := vaultrequests.NewRuntime(vaultrequests.RuntimeDependencies{
		Store: runtime.Requests.Store(ctx), Mutations: requestMutationPort{component: component, runtime: runtime},
		Prepare: actions.Prepare, AuthorizeOutput: actions.AuthorizeOutput,
		AllowRequest: func(tokenID int64) bool {
			return component.dependencies.AllowRequest("vault-request:" + runtime.Storage.DatabaseID + ":" + strconv.FormatInt(tokenID, 10))
		},
		Execute: actions.Execute, Compensate: actions.Compensate,
		RepairProjection: func(ctx context.Context, id int64) error {
			return runtime.Requests.RepairProjection(ctx, id)
		},
		RedactError: func(ctx context.Context, err error) string { return runtime.Requests.RedactRequestError(ctx, err) },
		IsStale:     actions.IsStale, MCPStarted: runtime.Session.MCPStarted,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault request runtime: %w", err)
	}
	return owner, nil
}
