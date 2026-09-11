package gatewayoperations

import (
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/console"
	consolehttp "github.com/aipermission/aipermission/backend/internal/console/httpapi"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayhttp"
	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func ObservationReportFormatVersion() string {
	return observationapp.ReportFormatVersion()
}

func NewBackupApplication(dependencies BackupDependencies) *BackupApplication {
	return &BackupApplication{Component: backupapp.New(backupapp.Dependencies(dependencies))}
}

func NewConsoleManager(db *sql.DB, openRuntime console.RuntimeOpener, redact func(string) string) *console.Manager {
	return console.NewManager(db, openRuntime, redact)
}

func NewMaintenanceHTTPHandlers(scope consolehttp.MaintenanceHTTPScopeProvider) *consolehttp.MaintenanceHTTPHandlers {
	return consolehttp.NewMaintenanceHTTPHandlers(scope)
}

func NewFileTransferHandlers(dependencies filetransferhttp.Dependencies) *filetransferhttp.Handlers {
	return filetransferhttp.NewHandlers(dependencies)
}

func IsStateChangingMethod(method string) bool {
	return gatewayhttp.IsStateChangingMethod(method)
}

func SetAttachmentHeaders(w http.ResponseWriter, filename string, contentType string) {
	httpattachment.SetHeaders(w, filename, contentType)
}

func NewMessageHTTPHandlers(scope messagequeue.ScopeProvider) *messagequeue.Handlers {
	return messagequeue.NewHTTPHandlers(scope)
}

func NewMessageStore(database *sql.DB, redact messagequeue.Redactor) *messagequeue.Store {
	return messagequeue.NewStore(database, redact)
}
