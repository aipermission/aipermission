package backupmigration

func UploadIdempotencyStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS backup_upload_operations (
			idempotency_key TEXT PRIMARY KEY,
			provider_id INTEGER NOT NULL,
			database_id TEXT NOT NULL,
			stream_id TEXT NOT NULL,
			source_installation_id TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('pending', 'dispatched', 'completed', 'outcome_unknown')),
			provider_file_id TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT,
			FOREIGN KEY(provider_id) REFERENCES backup_providers(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_backup_upload_operations_provider_status
			ON backup_upload_operations(provider_id, status, updated_at);`,
	}
}
