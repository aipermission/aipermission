// Package gatewayinfrastructure composes process and workspace owner
// capabilities for the API boundary.
package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

func InitializationError() error { return gatewayworkspace.InitializationError() }

type WorkspaceLifecyclePort interface{ gatewayworkspace.LifecyclePort }

type RuntimeIdentity struct {
	DatabaseID   string
	DatabasePath string
	WorkspaceID  string
	RuntimeID    string
	UIRetryID    string
}

func (identity RuntimeIdentity) Ready() bool {
	return identity.WorkspaceID != "" && identity.RuntimeID != ""
}

// componentIdentity makes handles component-scoped without retaining a
// token-to-runtime service locator. It must have non-zero size because pointers
// to distinct zero-size values may compare equal.
type componentIdentity struct{ _ byte }

// WorkspaceHandle is an opaque capability for one open encrypted workspace.
// Mutable resources remain owned by Component and are exposed only through
// feature-specific composition methods.
type WorkspaceHandle struct {
	component           *componentIdentity
	active              atomic.Bool
	identity            RuntimeIdentity
	workspace           *gatewayworkspace.Runtime
	access              gatewayworkspace.AccessCapabilities
	connectorActions    gatewayworkspace.ConnectorActionCapabilities
	connectorManagement gatewayworkspace.ConnectorManagementCapabilities
	connectorPorts      gatewayworkspace.ConnectorPortsCapabilities
	observation         gatewayworkspace.ObservationCapabilities
	operations          gatewayworkspace.OperationsCapabilities
	vault               gatewayworkspace.VaultCapabilities
}

func newWorkspaceHandle(component *componentIdentity, owner *gatewayworkspace.Runtime) *WorkspaceHandle {
	if owner == nil {
		return nil
	}
	identity := owner.Identity
	handle := &WorkspaceHandle{
		component: component,
		identity: RuntimeIdentity{
			DatabaseID: identity.DatabaseID, DatabasePath: identity.DatabasePath,
			WorkspaceID: identity.WorkspaceID, RuntimeID: identity.RuntimeID, UIRetryID: identity.UIRetryID,
		},
		workspace: owner,
		access: gatewayworkspace.AccessCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security,
		},
		connectorActions: gatewayworkspace.ConnectorActionCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security, Tag: owner.TagActionIdentity,
		},
		connectorManagement: gatewayworkspace.ConnectorManagementCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security,
		},
		connectorPorts: gatewayworkspace.ConnectorPortsCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security,
		},
		observation: gatewayworkspace.ObservationCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security, Observation: owner.Observation,
		},
		operations: gatewayworkspace.OperationsCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security,
		},
		vault: gatewayworkspace.VaultCapabilities{
			Storage: owner.Storage, Connectors: owner.Connectors, Security: owner.Security,
		},
	}
	handle.active.Store(true)
	return handle
}

func (handle *WorkspaceHandle) Identity() RuntimeIdentity {
	if handle == nil {
		return RuntimeIdentity{}
	}
	return handle.identity
}

func (handle *WorkspaceHandle) WorkspaceIdentity() Identity {
	if handle == nil {
		return Identity{}
	}
	return Identity{ID: handle.identity.DatabaseID, Path: handle.identity.DatabasePath, RetryIdentity: handle.identity.UIRetryID}
}

type ActionWorkflow interface {
	BeginShutdown()
	WaitShutdown(context.Context) error
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type CommandWorkflow interface {
	BeginWorkerShutdown()
	WaitWorkers(context.Context) error
	CancelRunning(context.Context, string) error
}

type TransferWorkflow interface {
	BeginShutdown() (bool, error)
	Wait(context.Context) bool
	Recover(context.Context, string, string) error
	Abort()
}

type WorkspaceDependencies struct {
	DataPath              string
	Open                  func(string, string, string) (*WorkspaceHandle, error)
	Close                 func(*WorkspaceHandle) error
	OnActivated, OnOpened func(*WorkspaceHandle)
	Move                  func(string, string) error
	Delete                func(string) error
	ValidateNewPassword   func(context.Context, *sql.DB, string, string) error
	Publish               func(string, string) error
	GatewaySecret         func() string
}

type PasswordAttempt interface {
	Success()
	Failure()
}

type WorkspaceHTTPDependencies struct {
	BeginAttempt     func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	HasSession       func(*http.Request) bool
	IssueSession     func(http.ResponseWriter) error
	ClearSessions    func(http.ResponseWriter)
	CloseMaintenance func(string)
	Now              func() time.Time
}

type WorkspaceHTTPHandlers interface {
	Status(http.ResponseWriter, *http.Request)
	Setup(http.ResponseWriter, *http.Request)
	Unlock(http.ResponseWriter, *http.Request)
	Lock(http.ResponseWriter, *http.Request)
	Rename(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
	DeleteLocked(http.ResponseWriter, *http.Request)
	Switch(http.ResponseWriter, *http.Request)
	ChangePassword(http.ResponseWriter, *http.Request)
}

type Identity struct {
	ID            string
	Path          string
	RetryIdentity string
}

type OpenWorkspaceInput struct{ input gatewayworkspace.OpenInput }

func NewOpenWorkspaceInput(id, path, password, gatewaySecret string, registry *connectors.Registry, adapters *connectorapi.Registry) OpenWorkspaceInput {
	return OpenWorkspaceInput{input: gatewayworkspace.OpenInput{
		ID: id, Path: path, Password: password, ConfiguredGatewaySecret: gatewaySecret,
		Registry: registry, AdapterRegistry: adapters,
	}}
}
