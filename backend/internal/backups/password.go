package backups

import "github.com/aipermission/aipermission/backend/internal/backups/passwordpolicy"

func ValidateRemoteBackupPassword(password, databaseName string) error {
	return passwordpolicy.Validate(password, databaseName)
}
