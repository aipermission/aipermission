// Package gatewayvault exposes project, secret, and Vault-session application contracts to the gateway.
package gatewayvault

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func InvalidatorUnavailableError() error { return vaultsessions.ErrInvalidatorUnavailable }

const (
	ActionGenerateItem = vaultrequests.ActionGenerateItem
)

type ProjectMutation func(context.Context, string, func() any, func(*sql.Tx) error) error

type ProjectScope struct {
	Database         *sql.DB
	Mutate           ProjectMutation
	AcquireExclusive func(context.Context) (func(), error)
	Invalidate       func(context.Context, int64, []SessionReference) error
}

type ProjectVaultHTTPScope struct {
	Runtime        ProjectVaultApplication
	RuntimeID      string
	SessionCatalog projectvault.SessionOptionsCatalog
}

type SessionMutationScope struct {
	ItemID    int64
	BindingID int64
}

type SessionReference struct {
	SessionID  int64
	RuntimeID  int64
	Generation int64
}

type SessionSelection struct {
	ItemID          int64
	SourceProjectID int64
	ReplaceExisting bool
	BindingID       int64
	BindingRevision int64
}

type VaultRequestApplication interface {
	List(context.Context, string, int) ([]vaultrequests.Request, error)
	Get(context.Context, int64) (vaultrequests.Request, error)
	RunPending(context.Context, int64, string) (vaultrequests.WorkflowResult, error)
	DeclinePending(context.Context, int64, string) (vaultrequests.Request, error)
	Call(context.Context, vaultrequests.CallInput) (vaultrequests.Request, error)
	DeliverCallResult(context.Context, int64, int64, func(vaultrequests.RequestView)) error
	DeliverOwned(context.Context, int64, int64, func(vaultrequests.RequestView)) error
	CancelOwned(context.Context, int64, int64) (vaultrequests.Request, error)
	StalePendingForContext(context.Context, int64, int64, string) error
	StalePendingForProject(context.Context, int64, string) error
	StalePendingForRuntimes(context.Context, []int64, string) error
	StalePendingForAction(context.Context, string, string) error
	FailRunning(context.Context, string) error
	Validate() error
}

type VaultApprovalHTTPScope struct {
	MCPStarted func() bool
	Runtime    func(context.Context) (VaultRequestApplication, error)
}

type VaultMCPHTTPScope struct {
	Database      *sql.DB
	Vault         *vault.Vault
	WorkspaceUUID string
	TokenID       int64
	MCPStarted    func() bool
	Runtime       func(context.Context) (VaultRequestApplication, error)
	MetadataRead  func(context.Context, int64) (bool, error)
}

type VaultSessionReference struct {
	SessionID  int64
	RuntimeID  int64
	Generation int64
}

type RequestInvalidator interface {
	StalePendingForContext(context.Context, int64, int64, string) error
	StalePendingForProject(context.Context, int64, string) error
	StalePendingForRuntimes(context.Context, []int64, string) error
}
