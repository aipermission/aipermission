// Package gatewayoperations exposes backup, observation, console, transfer, and message operations to the gateway.
package gatewayoperations

import (
	"github.com/aipermission/aipermission/backend/internal/console"
	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
)

type BackupApplication struct{ *backupapp.Component }
type BackupDependencies backupapp.Dependencies
type BackupRuntime = backupapp.Runtime
type ImportDatabaseRequest backupapp.ImportDatabaseRequest
type BackupPasswordAttempt = backupapp.PasswordAttempt
type TransientRestoreRequest backupapp.TransientRestoreRequest
type ObservationAppender = observationapp.Appender
type ObservationRuntime = observationapp.Runtime
type Observation struct{ observationapp.Component }
type MaintenanceConsoleRuntime = console.MaintenanceConsoleRuntime
type RuntimeOpenRequest = console.RuntimeOpenRequest
type RuntimeOpener = console.RuntimeOpener
type RuntimeSession = console.RuntimeSession
type SessionAuthorization = console.SessionAuthorization
type SessionHandle = console.SessionHandle
type SessionOperation = console.SessionOperation
