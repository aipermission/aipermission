package gatewayconnectoractions

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

// Workspace is the action-owner view of one unlocked workspace.
type Workspace struct {
	Storage  ActionStorage
	Identity ActionIdentity
	Workflow WorkflowPorts
}

type ActionStorage struct {
	Database    *sql.DB
	Tokens      *tokens.Store
	Registry    *connectors.Registry
	SecretVault *vault.Vault
	WorkspaceID string
}

type ActionIdentity struct {
	Key               []byte
	RuntimeInstanceID string
	MCPStarted        func() bool
	Ensure            func() error
}

type WorkflowPorts struct {
	Current       func() *actions.Runtime
	OrCreate      func(func() (*actions.Runtime, error)) (*actions.Runtime, error)
	AcquireSecret func(context.Context) (func(), error)
	RedactBasic   func(context.Context, string) string
	RedactCustom  func(context.Context, string) string
	Mutate        func(context.Context, string, *int64, int64, string, func() any, func(*sql.Tx) error) error
	Transaction   func(context.Context, func(*sql.Tx, actions.AuditAppender) error) error
	Observe       func(context.Context, string, *int64, int64, string, any)
	Capabilities  func(string, []actions.ResolvedDependency) connectors.RuntimeCapabilityResolver
	FinishRunning func(int64, actions.PreparedRequest, executionprincipal.Principal, connectors.ActionHandles)
}

func (workspace Workspace) workflowReady() bool {
	return workspace.Storage.Database != nil && workspace.Storage.Tokens != nil && workspace.Storage.Registry != nil &&
		workspace.Storage.SecretVault != nil && workspace.Identity.MCPStarted != nil && workspace.Identity.Ensure != nil &&
		workspace.Workflow.OrCreate != nil && workspace.Workflow.AcquireSecret != nil && workspace.Workflow.RedactBasic != nil &&
		workspace.Workflow.RedactCustom != nil && workspace.Workflow.Mutate != nil && workspace.Workflow.Transaction != nil &&
		workspace.Workflow.Observe != nil && workspace.Workflow.Capabilities != nil && workspace.Workflow.FinishRunning != nil
}
