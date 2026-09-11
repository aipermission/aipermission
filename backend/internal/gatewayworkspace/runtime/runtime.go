package runtime

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimeinput"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	runtimeshutdown "github.com/aipermission/aipermission/backend/internal/workspaceruntime/shutdown"
)

// Runtime is the constructor boundary returned to workspace lifecycle code.
// The named interface prevents the concrete workspaceruntime implementation
// from becoming part of the public gateway contract.
type Runtime interface {
	runtimecontract.Runtime
}

type ActionWorkflow = runtimeshutdown.ActionWorkflow
type CommandWorkflow = runtimeshutdown.CommandWorkflow

func Adopt(ctx context.Context, input runtimeinput.Adopt) (Runtime, error) {
	state, err := foundation.Adopt(ctx, foundation.AdoptInput{
		ID: input.ID, Path: input.Path, Database: input.Database, Vault: input.Vault,
		TokenStore: input.TokenStore, ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry, RuntimeInstanceID: input.RuntimeInstanceID,
	})
	if err != nil {
		return nil, err
	}
	return workspaceruntime.New(state), nil
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
	return workspaceruntime.New(state), nil
}

func Discard(value Runtime) error {
	return runtimeshutdown.Discard(value)
}

func Close(value Runtime, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error)) error {
	return runtimeshutdown.Close(value, resolveActions, resolveCommands)
}
