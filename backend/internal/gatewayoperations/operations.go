// Package gatewayoperations exposes backup, observation, console, transfer, and message operations to the gateway.
package gatewayoperations

import (
	"github.com/aipermission/aipermission/backend/internal/applicationbackup"
	"github.com/aipermission/aipermission/backend/internal/applicationobservation"
	"github.com/aipermission/aipermission/backend/internal/console"
	consolehttp "github.com/aipermission/aipermission/backend/internal/console/httpapi"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayhttp"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

var (
	ObservationReportFormatVersion = applicationobservation.ReportFormatVersion
	NewBackupApplication           = applicationbackup.New
	NewConsoleManager              = console.NewManager
	NewMaintenanceHTTPHandlers     = consolehttp.NewMaintenanceHTTPHandlers
	NewFileTransferHandlers        = filetransferhttp.NewHandlers
	IsStateChangingMethod          = gatewayhttp.IsStateChangingMethod
	SetAttachmentHeaders           = httpattachment.SetHeaders
	NewMessageHTTPHandlers         = messagequeue.NewHTTPHandlers
	NewMessageStore                = messagequeue.NewStore
)

type BackupApplication = applicationbackup.Component
type BackupDependencies = applicationbackup.Dependencies
type ImportDatabaseRequest = applicationbackup.ImportDatabaseRequest
type BackupPasswordAttempt = applicationbackup.PasswordAttempt
type TransientRestoreRequest = applicationbackup.TransientRestoreRequest
type ObservationAppender = applicationobservation.Appender
type Observation = applicationobservation.Component
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
