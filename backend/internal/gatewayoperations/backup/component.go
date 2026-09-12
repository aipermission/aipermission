// Package backup composes encrypted workspace import, export, and remote
// provider operations behind the gateway operations boundary.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

var ErrOperationUnavailable = errors.New("backup operation lease is unavailable")
var ErrLifecycleUnavailable = errors.New("workspace lifecycle lease is unavailable")

const defaultConcurrentOperations = 2

// OperationLimiter owns the process-wide concurrency boundary for encrypted
// backup and restore operations.
type OperationLimiter struct {
	once  sync.Once
	slots chan struct{}
}

func (limiter *OperationLimiter) Acquire(ctx context.Context) (func(), error) {
	limiter.once.Do(func() { limiter.slots = make(chan struct{}, defaultConcurrentOperations) })
	select {
	case limiter.slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-limiter.slots }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type Lifecycle interface {
	AcquireReadContext(context.Context) (func(), error)
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

type readOperationLease struct {
	releaseLifecycle func()
	releaseOperation func()
}

func (component *Component) acquireReadOperation(ctx context.Context) (*readOperationLease, error) {
	if component == nil || component.dependencies.Lifecycle == nil {
		return nil, ErrLifecycleUnavailable
	}
	releaseLifecycle, err := component.dependencies.Lifecycle.AcquireReadContext(ctx)
	if err != nil {
		return nil, err
	}
	releaseOperation, err := component.dependencies.AcquireOperation(ctx)
	if err != nil {
		releaseLifecycle()
		return nil, err
	}
	return &readOperationLease{releaseLifecycle: releaseLifecycle, releaseOperation: releaseOperation}, nil
}

func (lease *readOperationLease) ReleaseLifecycle() {
	if lease != nil && lease.releaseLifecycle != nil {
		lease.releaseLifecycle()
		lease.releaseLifecycle = nil
	}
}

func (lease *readOperationLease) Release() {
	if lease == nil {
		return
	}
	if lease.releaseOperation != nil {
		lease.releaseOperation()
		lease.releaseOperation = nil
	}
	lease.ReleaseLifecycle()
}

func New(dependencies Dependencies) *Component {
	if dependencies.AcquireOperation == nil {
		dependencies.AcquireOperation = func(context.Context) (func(), error) {
			return nil, ErrOperationUnavailable
		}
	}
	return &Component{dependencies: dependencies}
}

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
