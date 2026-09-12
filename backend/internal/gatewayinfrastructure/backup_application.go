package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewaybackup "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
)

type BackupRuntimePorts struct {
	Mutate        func(context.Context, string, func() any, func(*sql.Tx) error) error
	AuditRequired func(context.Context, string, any) error
	Observe       func(context.Context, string, any)
}

type BackupApplicationDependencies struct {
	DataPath            string
	ActiveRuntime       func(http.ResponseWriter) (*WorkspaceHandle, BackupRuntimePorts, bool)
	CurrentDatabaseName func() string
	HasSession          func(*http.Request) bool
	BeginAttempt        func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	IssuePrepared       func(http.ResponseWriter, gatewayaccess.PreparedUISession) error
}

type BackupApplication struct{ owner *gatewaybackup.Component }

type Handler func(http.ResponseWriter, *http.Request)

type TransientBackup interface {
	List(http.ResponseWriter, *http.Request)
}

type BackupProviders interface {
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
	Download        Handler
	Import          Handler
	RestoreRemote   Handler
	RestoreProvider Handler
	Providers       BackupProviders
	Transient       TransientBackup
}

func (component *OperationsOwner) NewBackupApplication(dependencies BackupApplicationDependencies) *BackupApplication {
	owner := gatewaybackup.New(gatewaybackup.Dependencies{
		DataPath: dependencies.DataPath, Lifecycle: component.owner.WorkspaceOwner().WorkspaceLifecycle(),
		ActiveRuntime: func(w http.ResponseWriter) (gatewaybackup.Runtime, bool) {
			handle, ports, ok := dependencies.ActiveRuntime(w)
			if !ok {
				return gatewaybackup.Runtime{}, false
			}
			return component.backupWorkspace(handle, gatewaybackup.Runtime{
				Mutate: ports.Mutate, AuditRequired: ports.AuditRequired, Observe: ports.Observe,
			})
		},
		CurrentDatabaseName: dependencies.CurrentDatabaseName,
		HasSession:          dependencies.HasSession,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewaybackup.PasswordAttempt, bool) {
			return dependencies.BeginAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared gatewayaccess.PreparedUISession) error {
			return dependencies.IssuePrepared(w, prepared)
		},
		AcquireOperation: component.owner.WorkspaceOwner().AcquireBackupOperation,
	})
	return &BackupApplication{owner: owner}
}

func (application *BackupApplication) HTTPHandlers() BackupHTTPHandlers {
	handlers := application.owner.HTTPHandlers()
	return BackupHTTPHandlers{
		Download: Handler(handlers.Download), Import: Handler(handlers.Import),
		RestoreRemote: Handler(handlers.RestoreRemote), RestoreProvider: Handler(handlers.RestoreProvider),
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
	application.owner.InstallImportedDatabase(w, r, databaseName, password, writeTemp, mutate)
}
