package db

var fileTransferStartIdempotencyMigration = migration{
	version:     28,
	description: "file transfer start idempotency",
	statements: []string{
		`CREATE TABLE IF NOT EXISTS file_transfer_start_idempotency (
			scope TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			identity_hash TEXT NOT NULL,
			resource_kind TEXT NOT NULL CHECK (resource_kind IN ('transfer', 'batch')),
			resource_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			PRIMARY KEY (scope, idempotency_key)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_file_transfer_start_idempotency_expires
			ON file_transfer_start_idempotency(expires_at);`,
	},
}
