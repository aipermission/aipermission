// Package gatewayinfrastructure exposes process, routing, and workspace composition contracts to the API boundary.
package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"time"

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

// workspaceToken must have non-zero size. Go permits pointers to distinct
// zero-size values to compare equal, which would collapse independent
// workspace capabilities into one map key.
type workspaceToken struct{ _ byte }

// WorkspaceHandle is an opaque capability for one open encrypted workspace.
// Mutable resources remain owned by Component and are exposed only through
// feature-specific composition methods.
type WorkspaceHandle struct {
	token    *workspaceToken
	identity RuntimeIdentity
}

func newWorkspaceHandle(owner *gatewayworkspace.Runtime) *WorkspaceHandle {
	if owner == nil {
		return nil
	}
	identity := owner.Identity
	return &WorkspaceHandle{
		token: &workspaceToken{},
		identity: RuntimeIdentity{
			DatabaseID: identity.DatabaseID, DatabasePath: identity.DatabasePath,
			WorkspaceID: identity.WorkspaceID, RuntimeID: identity.RuntimeID, UIRetryID: identity.UIRetryID,
		},
	}
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
