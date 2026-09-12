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
	AcquireMutationContext(context.Context) (func(), error)
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

type lifecycleOperationLease struct {
	releaseLifecycle func()
	releaseOperation func()
}

func (component *Component) acquireLifecycleOperation(ctx context.Context, acquireLifecycle func(context.Context) (func(), error)) (*lifecycleOperationLease, error) {
	if component == nil || component.dependencies.Lifecycle == nil {
		return nil, ErrLifecycleUnavailable
	}
	releaseOperation, err := component.dependencies.AcquireOperation(ctx)
	if err != nil {
		return nil, err
	}
	releaseLifecycle, err := acquireLifecycle(ctx)
	if err != nil {
		releaseOperation()
		return nil, err
	}
	return &lifecycleOperationLease{releaseLifecycle: releaseLifecycle, releaseOperation: releaseOperation}, nil
}

func (component *Component) acquireReadOperation(ctx context.Context) (*lifecycleOperationLease, error) {
	if component == nil || component.dependencies.Lifecycle == nil {
		return nil, ErrLifecycleUnavailable
	}
	return component.acquireLifecycleOperation(ctx, component.dependencies.Lifecycle.AcquireReadContext)
}

func (component *Component) acquireMutationOperation(ctx context.Context) (*lifecycleOperationLease, error) {
	if component == nil || component.dependencies.Lifecycle == nil {
		return nil, ErrLifecycleUnavailable
	}
	return component.acquireLifecycleOperation(ctx, component.dependencies.Lifecycle.AcquireMutationContext)
}

func (lease *lifecycleOperationLease) ReleaseLifecycle() {
	if lease != nil && lease.releaseLifecycle != nil {
		lease.releaseLifecycle()
		lease.releaseLifecycle = nil
	}
}

func (lease *lifecycleOperationLease) Release() {
	if lease == nil {
		return
	}
	lease.ReleaseLifecycle()
	if lease.releaseOperation != nil {
		lease.releaseOperation()
		lease.releaseOperation = nil
	}
}

func New(dependencies Dependencies) *Component {
	if dependencies.AcquireOperation == nil {
		dependencies.AcquireOperation = func(context.Context) (func(), error) {
			return nil, ErrOperationUnavailable
		}
	}
	return &Component{dependencies: dependencies}
}

// ValidateNewPassword applies backup-specific password requirements only when
// the workspace has an active remote backup provider.
func ValidateNewPassword(ctx context.Context, database *sql.DB, databaseName, password string) error {
	active, err := backups.NewStore(database).HasActiveProvider(ctx)
	if err != nil || !active {
		return err
	}
	if err := backups.ValidateRemoteBackupPassword(password, databaseName); err != nil {
		return workspacelifecycle.PasswordPolicyError(err)
	}
	return nil
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
		Providers:       backups.NewHTTPHandlers(component.providerScope, component.providerOperationScope),
		Transient:       backups.NewTransientHTTPHandlers(),
	}
}
