// Package gatewayoperations exposes backup, observation, console, transfer, and message operations to the gateway.
package gatewayoperations

import (
	"github.com/aipermission/aipermission/backend/internal/console"
	consolehttp "github.com/aipermission/aipermission/backend/internal/console/httpapi"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayhttp"
	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

type BackupApplication struct{ *backupapp.Component }
type BackupDependencies backupapp.Dependencies
type BackupRuntime = backupapp.Runtime
type ImportDatabaseRequest backupapp.ImportDatabaseRequest
type BackupPasswordAttempt = backupapp.PasswordAttempt
type TransientRestoreRequest backupapp.TransientRestoreRequest
type ObservationAppender = observationapp.Appender
type Observation struct{ observationapp.Component }
type MaintenanceConsoleRuntime = console.MaintenanceConsoleRuntime
type RuntimeOpenRequest = console.RuntimeOpenRequest
type RuntimeOpener = console.RuntimeOpener
type RuntimeSession = console.RuntimeSession
type SessionAuthorization = console.SessionAuthorization
type SessionHandle = console.SessionHandle
type SessionOperation = console.SessionOperation
type MaintenanceHTTPScope = consolehttp.MaintenanceHTTPScope
type FileTransferConnectorPorts = filetransferhttp.ConnectorPorts
type FileTransferDependencies = filetransferhttp.Dependencies
type FileTransferHandlers = filetransferhttp.Handlers
type FileTransferRuntime = filetransferhttp.Runtime
type HTTPBoundary = gatewayhttp.Boundary
type MessageScope = messagequeue.Scope
type MessageStore = messagequeue.Store
