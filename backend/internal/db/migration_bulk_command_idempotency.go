package db

var bulkCommandIdempotencyMigration = migration{
	version: 29, description: "bulk command idempotency", statements: []string{
		`CREATE TABLE IF NOT EXISTS bulk_command_idempotency (
			idempotency_key TEXT PRIMARY KEY,
			identity_hash TEXT NOT NULL,
			response_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bulk_command_idempotency_expires
			ON bulk_command_idempotency(expires_at);`,
	},
}
