package gatewayoperations

import backupapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"

func NewBackupApplication(dependencies BackupDependencies) *BackupApplication {
	return &BackupApplication{Component: backupapp.New(backupapp.Dependencies(dependencies))}
}
