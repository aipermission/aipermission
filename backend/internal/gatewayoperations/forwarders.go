package gatewayoperations

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/console"
	backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func NewBackupApplication(dependencies BackupDependencies) *BackupApplication {
	return &BackupApplication{Component: backupapp.New(backupapp.Dependencies(dependencies))}
}

func NewConsoleManager(db *sql.DB, openRuntime console.RuntimeOpener, redact func(string) string) *console.Manager {
	return console.NewManager(db, openRuntime, redact)
}

func NewMessageHTTPHandlers(scope messagequeue.ScopeProvider) *messagequeue.Handlers {
	return messagequeue.NewHTTPHandlers(scope)
}

func NewMessageStore(database *sql.DB, redact messagequeue.Redactor) *messagequeue.Store {
	return messagequeue.NewStore(database, redact)
}
