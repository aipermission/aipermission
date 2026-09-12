package gatewayvault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
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
	DatabaseID  string
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
	InvalidateSessions func(context.Context, []SessionReference, SessionMutationScope) error
	SessionEnvironment func(context.Context, int64) (bool, error)
	Mutate             func(context.Context, string, func() any, func(*sql.Tx) error) error
	Observe            func(context.Context, string, any) error
}

type ActionRuntimePorts struct {
	Connector ConnectorPort
	Mutate    func(context.Context, int64, string, func() any, func(*sql.Tx) error) error
}

type RequestRuntimePorts struct {
	Store              RequestStoreFactory
	Mutate             func(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Observe            func(context.Context, string, *int64, int64, string, any)
	RepairProjection   func(context.Context, int64) error
	RedactRequestError func(context.Context, error) string
}

type RequestStoreFactory func(context.Context) vaultrequests.RequestStore

// ConnectorPort supplies connector facts to Vault. Execution policy remains
// owned by this package so transport composition cannot silently broaden it.
type ConnectorPort interface {
	SessionEnvironmentVersion(context.Context, int64) (string, error)
	LiveConsolePermission(context.Context, int64, int64, int64, string) (connectortargets.ActionPermission, string, error)
	ExpectedPeerIdentities(context.Context, connectortargets.RuntimeSurface) (PeerIdentityExpectation, error)
}

type PeerIdentityExpectation struct {
	Items    []string
	Required bool
}
