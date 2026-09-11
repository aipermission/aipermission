// Package backup composes encrypted workspace import, export, and remote
// provider operations behind the gateway operations boundary.
package backup

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Lifecycle interface {
	AcquireRead() func()
	Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
}

type PasswordAttempt interface {
	Success()
	Failure()
}

type Runtime struct {
	Database      *sql.DB
	SecretVault   *vault.Vault
	DatabaseID    string
	DatabasePath  string
	WorkspaceID   string
	Mutate        func(context.Context, string, func() any, func(*sql.Tx) error) error
	AuditRequired func(context.Context, string, any) error
	Observe       func(context.Context, string, any)
}

type Dependencies struct {
	DataPath            string
	Lifecycle           Lifecycle
	ActiveRuntime       func(http.ResponseWriter) (Runtime, bool)
	CurrentDatabaseName func() string
	HasSession          func(*http.Request) bool
	BeginAttempt        func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	IssuePrepared       func(http.ResponseWriter, uisession.Prepared) error
	AcquireOperation    backups.OperationLease
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
