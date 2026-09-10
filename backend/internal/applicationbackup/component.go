// Package applicationbackup composes encrypted workspace import, export, and
// remote backup provider operations behind a narrow HTTP boundary.
package applicationbackup

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type Lifecycle interface {
	AcquireRead() func()
	Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
}

type PasswordAttempt interface {
	Success()
	Failure()
}

type Dependencies struct {
	DataPath            string
	Lifecycle           Lifecycle
	ActiveRuntime       func(http.ResponseWriter) (*workspaceruntime.Runtime, bool)
	CurrentDatabaseName func() string
	HasSession          func(*http.Request) bool
	BeginAttempt        func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	IssuePrepared       func(http.ResponseWriter, uisession.Prepared) error
	AcquireOperation    backups.OperationLease
	Mutate              func(context.Context, *workspaceruntime.Runtime, string, func() any, func(*sql.Tx) error) error
	AuditRequired       func(context.Context, *workspaceruntime.Runtime, string, any) error
	Observe             func(context.Context, *workspaceruntime.Runtime, string, any)
}

type Component struct{ dependencies Dependencies }

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

type Handlers struct {
	Download        http.HandlerFunc
	Import          http.HandlerFunc
	RestoreRemote   http.HandlerFunc
	RestoreProvider http.HandlerFunc
	Providers       *backups.HTTPHandlers
	Transient       *backups.TransientHTTPHandlers
}

func (component *Component) HTTPHandlers() Handlers {
	return Handlers{
		Download: component.downloadDatabase, Import: component.importDatabase,
		RestoreRemote:   component.restoreTransientRemoteBackup,
		RestoreProvider: component.restoreProviderRecord,
		Providers:       backups.NewHTTPHandlers(component.providerScope),
		Transient:       backups.NewTransientHTTPHandlers(),
	}
}
