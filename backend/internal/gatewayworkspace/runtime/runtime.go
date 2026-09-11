package runtime

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	runtimeshutdown "github.com/aipermission/aipermission/backend/internal/workspaceruntime/shutdown"
)

type Runtime = workspaceruntime.Port
type Vault = vault.Vault
type TokenStore = tokens.Store
type ActionWorkflow = runtimeshutdown.ActionWorkflow

type AdoptInput struct {
	ID, Path, ConfiguredGatewaySecret string
	Database                          *sql.DB
	Vault                             *Vault
	TokenStore                        *TokenStore
	Registry                          *connectors.Registry
	AdapterRegistry                   *connectorapi.Registry
	RuntimeInstanceID                 func() (string, error)
}

type OpenInput struct {
	ID, Path, Password, ConfiguredGatewaySecret string
	Registry                                    *connectors.Registry
	AdapterRegistry                             *connectorapi.Registry
}

func Adopt(ctx context.Context, input AdoptInput) (Runtime, error) {
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

func Open(ctx context.Context, input OpenInput) (Runtime, error) {
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

func Close(value Runtime, resolve func() (ActionWorkflow, error)) error {
	return runtimeshutdown.Close(value, resolve)
}

var _ Runtime = (*workspaceruntime.Runtime)(nil)
