package db

import "database/sql"

func fileTransferRecoveryMigration() migration {
	return migration{
		version: 31, description: "durable file transfer recovery metadata", preflight: ensureFileTransferRecoveryColumns,
		statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_file_transfers_temp_expiry ON file_transfers(temp_expires_at, id) WHERE temp_path != '';`,
			`CREATE INDEX IF NOT EXISTS idx_file_transfers_remote_staging ON file_transfers(id) WHERE remote_staging_ref != '';`,
			`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_archive_expiry ON file_transfer_batches(archive_expires_at, id) WHERE archive_path != '';`,
		},
	}
}

func ensureFileTransferRecoveryColumns(tx *sql.Tx) error {
	columns := [][3]string{
		{"file_transfers", "failure_details_json", `ALTER TABLE file_transfers ADD COLUMN failure_details_json TEXT NOT NULL DEFAULT '{}'`},
		{"file_transfers", "temp_expires_at", `ALTER TABLE file_transfers ADD COLUMN temp_expires_at TEXT`},
		{"file_transfers", "remote_staging_ref", `ALTER TABLE file_transfers ADD COLUMN remote_staging_ref TEXT NOT NULL DEFAULT ''`},
		{"file_transfer_batches", "archive_expires_at", `ALTER TABLE file_transfer_batches ADD COLUMN archive_expires_at TEXT`},
	}
	for _, column := range columns {
		if err := ensureColumn(tx, column[0], column[1], column[2]); err != nil {
			return err
		}
	}
	return nil
}
