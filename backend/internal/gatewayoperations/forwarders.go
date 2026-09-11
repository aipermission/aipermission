package gatewayoperations

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/console"
	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
)

func NewBackupApplication(dependencies BackupDependencies) *BackupApplication {
	return &BackupApplication{Component: backupapp.New(backupapp.Dependencies(dependencies))}
}

func NewConsoleManager(db *sql.DB, openRuntime console.RuntimeOpener, redact func(string) string) *console.Manager {
	return console.NewManager(db, openRuntime, redact)
}
