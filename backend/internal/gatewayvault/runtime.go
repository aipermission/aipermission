package gatewayvault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	runtimeops "github.com/aipermission/aipermission/backend/internal/workspaceruntime/operations"
)

// Runtime is the Vault-owned capability view of one unlocked workspace.
// Callbacks are bound by the composition root so Vault workflows cannot reach
// unrelated workspace services.
type Runtime struct {
	Storage  StorageRuntime
	Session  SessionRuntime
	Project  ProjectRuntimePorts
	Action   ActionRuntimePorts
	Requests RequestRuntimePorts
}

type StorageRuntime struct {
	Database    *sql.DB
	SecretVault *vault.Vault
	Tokens      vaultactions.TokenReader
	WorkspaceID string
}

type SessionRuntime struct {
	Sessions          vaultactions.SessionPort
	Leases            vaultactions.LeasePort
	RuntimeInstanceID string
	MCPStarted        func() bool
	AcquireDelivery   func(context.Context) (func(), error)
	AcquireExclusive  func(context.Context) (func(), error)
}

type ProjectRuntimePorts struct {
	State              *runtimeops.StateSlot
	InvalidateSessions func(context.Context, []projectvault.SessionReference, projectvault.SessionMutationScope) error
	SessionEnvironment func(context.Context, int64) (bool, error)
	Mutate             func(context.Context, string, func() any, func(*sql.Tx) error) error
	Observe            func(context.Context, string, any) error
}

type ActionRuntimePorts struct {
	Connector     vaultactions.ConnectorPort
	Mutate        func(context.Context, int64, string, func() any, func(*sql.Tx) error) error
	AllowGenerate func(int64) bool
}

type RequestRuntimePorts struct {
	Store              func(context.Context) *vaultrequests.Store
	Mutate             func(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Observe            func(context.Context, string, *int64, int64, string, any)
	AllowRequest       func(int64) bool
	RepairProjection   func(context.Context, int64) error
	RedactRequestError func(context.Context, error) string
}
