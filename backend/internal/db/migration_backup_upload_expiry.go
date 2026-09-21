package db

import "github.com/aipermission/aipermission/backend/internal/db/backupmigration"

var backupUploadIdempotencyMigration = migration{
	version: 30, description: "backup upload idempotency", statements: backupmigration.UploadIdempotencyStatements(),
}

func backupUploadExpiryMigration() migration {
	return migration{
		version: 36, description: "terminal backup upload expiry", statements: backupmigration.UploadExpiryStatements(),
	}
}

func backupUploadWorkspaceInstanceMigration() migration {
	return migration{
		version: 38, description: "backup upload workspace incarnation", statements: backupmigration.UploadWorkspaceInstanceStatements(),
	}
}
