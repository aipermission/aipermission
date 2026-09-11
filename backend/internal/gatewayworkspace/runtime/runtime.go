package runtime

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimeinput"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/gatewayadapter"
	runtimeshutdown "github.com/aipermission/aipermission/backend/internal/workspaceruntime/shutdown"
)

var errForeignRuntime = errors.New("workspace runtime was not created by the gateway workspace factory")

type Runtime = runtimecontract.Runtime

type ActionWorkflow = runtimeshutdown.ActionWorkflow

func Adopt(ctx context.Context, input runtimeinput.Adopt) (Runtime, error) {
	state, err := foundation.Adopt(ctx, foundation.AdoptInput{
		ID: input.ID, Path: input.Path, Database: input.Database, Vault: input.Vault,
		TokenStore: input.TokenStore, ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry, RuntimeInstanceID: input.RuntimeInstanceID,
	})
	if err != nil {
		return nil, err
	}
	return gatewayadapter.Wrap(workspaceruntime.New(state)), nil
}

func Open(ctx context.Context, input runtimeinput.Open) (Runtime, error) {
	state, err := foundation.Open(ctx, foundation.OpenInput{
		ID: input.ID, Path: input.Path, Password: input.Password,
		ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry:                input.Registry, AdapterRegistry: input.AdapterRegistry,
	})
	if err != nil {
		return nil, err
	}
	return gatewayadapter.Wrap(workspaceruntime.New(state)), nil
}

func Discard(value Runtime) error {
	runtime, err := concreteRuntime(value)
	if err != nil {
		return err
	}
	return runtimeshutdown.Discard(runtime)
}

func Close(value Runtime, resolve func() (ActionWorkflow, error)) error {
	runtime, err := concreteRuntime(value)
	if err != nil {
		return err
	}
	return runtimeshutdown.Close(runtime, resolve)
}

func concreteRuntime(value Runtime) (*workspaceruntime.Runtime, error) {
	if value == nil {
		return nil, nil
	}
	runtime, ok := gatewayadapter.Unwrap(value)
	if !ok {
		return nil, errForeignRuntime
	}
	return runtime, nil
}
