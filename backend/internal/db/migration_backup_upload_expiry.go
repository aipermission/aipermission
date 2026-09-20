package db

import "github.com/aipermission/aipermission/backend/internal/db/backupmigration"

func backupUploadExpiryMigration() migration {
	return migration{version: 36, description: "terminal backup upload expiry", statements: backupmigration.UploadExpiryStatements()}
}
