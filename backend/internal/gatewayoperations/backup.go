package gatewayoperations

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

var ErrBackupOperationUnavailable = errors.New("backup operation lease is unavailable")

type BackupLifecycle interface {
	AcquireRead() func()
	Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
}

type BackupPasswordAttempt interface {
	Success()
	Failure()
}

type BackupRuntime struct {
	Database      *sql.DB
	SecretVault   *vault.Vault
	DatabaseID    string
	DatabasePath  string
	WorkspaceID   string
	Mutate        func(context.Context, string, func() any, func(*sql.Tx) error) error
	AuditRequired func(context.Context, string, any) error
	Observe       func(context.Context, string, any)
}

type BackupDependencies struct {
	DataPath            string
	Lifecycle           BackupLifecycle
	ActiveRuntime       func(http.ResponseWriter) (BackupRuntime, bool)
	CurrentDatabaseName func() string
	HasSession          func(*http.Request) bool
	BeginAttempt        func(http.ResponseWriter, *http.Request) (BackupPasswordAttempt, bool)
	IssuePrepared       func(http.ResponseWriter, uisession.Prepared) error
	AcquireOperation    func(context.Context) (func(), error)
}

type ImportDatabaseRequest struct {
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

type TransientRestoreRequest struct {
	BaseURL          string `json:"base_url"`
	Token            string `json:"token"`
	StreamID         string `json:"stream_id"`
	BackupID         string `json:"backup_id"`
	DatabaseName     string `json:"database_name"`
	DatabasePassword string `json:"database_password"`
}

type TransientBackupHTTPHandlers interface {
	List(http.ResponseWriter, *http.Request)
}

type BackupProviderHTTPHandlers interface {
	ProviderCatalog(http.ResponseWriter, *http.Request)
	ListProviders(http.ResponseWriter, *http.Request)
	BackupFreshness(http.ResponseWriter, *http.Request)
	CreateProvider(http.ResponseWriter, *http.Request)
	UpdateProvider(http.ResponseWriter, *http.Request)
	DeleteProvider(http.ResponseWriter, *http.Request)
	EnableProvider(http.ResponseWriter, *http.Request)
	TestProvider(http.ResponseWriter, *http.Request)
	ListProviderRecords(http.ResponseWriter, *http.Request)
	UploadProviderBackup(http.ResponseWriter, *http.Request)
	PruneProviderBackups(http.ResponseWriter, *http.Request)
	BackupProviderStorage(http.ResponseWriter, *http.Request)
	BackupProviderRetention(http.ResponseWriter, *http.Request)
	PreviewBackupProviderRetention(http.ResponseWriter, *http.Request)
	UpdateBackupProviderRetention(http.ResponseWriter, *http.Request)
	DeleteProviderBackupRecords(http.ResponseWriter, *http.Request)
	DownloadProviderRecord(http.ResponseWriter, *http.Request)
}

type BackupHTTPHandlers struct {
	Download        http.HandlerFunc
	Import          http.HandlerFunc
	RestoreRemote   http.HandlerFunc
	RestoreProvider http.HandlerFunc
	Providers       BackupProviderHTTPHandlers
	Transient       TransientBackupHTTPHandlers
}

type BackupApplication struct {
	component *backupapp.Component
}

func NewBackupApplication(dependencies BackupDependencies) *BackupApplication {
	activeRuntime := func(w http.ResponseWriter) (backupapp.Runtime, bool) {
		if dependencies.ActiveRuntime == nil {
			return backupapp.Runtime{}, false
		}
		runtime, ok := dependencies.ActiveRuntime(w)
		return backupapp.Runtime{
			Database: runtime.Database, SecretVault: runtime.SecretVault,
			DatabaseID: runtime.DatabaseID, DatabasePath: runtime.DatabasePath, WorkspaceID: runtime.WorkspaceID,
			Mutate: runtime.Mutate, AuditRequired: runtime.AuditRequired, Observe: runtime.Observe,
		}, ok
	}
	beginAttempt := func(w http.ResponseWriter, r *http.Request) (backupapp.PasswordAttempt, bool) {
		if dependencies.BeginAttempt == nil {
			return nil, false
		}
		attempt, ok := dependencies.BeginAttempt(w, r)
		return attempt, ok
	}
	acquireOperation := func(ctx context.Context) (func(), error) {
		if dependencies.AcquireOperation == nil {
			return nil, ErrBackupOperationUnavailable
		}
		return dependencies.AcquireOperation(ctx)
	}
	component := backupapp.New(backupapp.Dependencies{
		DataPath: dependencies.DataPath, Lifecycle: dependencies.Lifecycle, ActiveRuntime: activeRuntime,
		CurrentDatabaseName: dependencies.CurrentDatabaseName, HasSession: dependencies.HasSession,
		BeginAttempt: beginAttempt, IssuePrepared: dependencies.IssuePrepared,
		AcquireOperation: acquireOperation,
	})
	return &BackupApplication{component: component}
}

func (application *BackupApplication) HTTPHandlers() BackupHTTPHandlers {
	if application == nil || application.component == nil {
		return BackupHTTPHandlers{}
	}
	handlers := application.component.HTTPHandlers()
	return BackupHTTPHandlers{
		Download: handlers.Download, Import: handlers.Import,
		RestoreRemote: handlers.RestoreRemote, RestoreProvider: handlers.RestoreProvider,
		Providers: handlers.Providers, Transient: handlers.Transient,
	}
}

func (application *BackupApplication) InstallImportedDatabase(
	w http.ResponseWriter,
	r *http.Request,
	databaseName string,
	password string,
	writeTemp func(string) error,
	mutate func(*sql.DB) error,
) {
	if application == nil || application.component == nil {
		return
	}
	application.component.InstallImportedDatabase(w, r, databaseName, password, writeTemp, mutate)
}
