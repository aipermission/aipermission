package backupmigration

func UploadExpiryStatements() []string {
	return []string{
		`CREATE TABLE backup_upload_operations_v35 (
			idempotency_key TEXT PRIMARY KEY,
			provider_id INTEGER NOT NULL,
			database_id TEXT NOT NULL,
			stream_id TEXT NOT NULL,
			source_installation_id TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('pending', 'dispatched', 'completed', 'outcome_unknown', 'expired')),
			provider_file_id TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT,
			FOREIGN KEY(provider_id) REFERENCES backup_providers(id) ON DELETE CASCADE
		);`,
		`INSERT INTO backup_upload_operations_v35 (
			idempotency_key, provider_id, database_id, stream_id, source_installation_id,
			status, provider_file_id, last_error, created_at, updated_at, completed_at
		)
		SELECT o.idempotency_key, o.provider_id, o.database_id, o.stream_id, o.source_installation_id,
		       CASE
		           WHEN o.status IN ('pending', 'dispatched', 'outcome_unknown') THEN 'expired'
		           WHEN o.status = 'completed' AND o.remote_deleted THEN 'expired'
		           ELSE o.status
		       END,
		       o.provider_file_id,
		       CASE
		           WHEN o.status IN ('pending', 'dispatched', 'outcome_unknown')
		               THEN 'upload outcome expired during protocol upgrade'
		           WHEN o.status = 'completed' AND o.remote_deleted AND TRIM(o.last_error) = ''
		               THEN 'remote upload result expired'
		           ELSE o.last_error
		       END,
		       o.created_at, o.updated_at,
		       CASE WHEN o.status IN ('pending', 'dispatched', 'outcome_unknown')
		                 OR (o.status = 'completed' AND o.remote_deleted)
		           THEN COALESCE(o.completed_at, o.updated_at, o.created_at) ELSE o.completed_at END
		FROM (
			SELECT source.*,
			       EXISTS (
			           SELECT 1 FROM backup_records r
			           WHERE r.provider_id = source.provider_id
			             AND r.provider_file_id = source.provider_file_id
			             AND r.deleted_at IS NOT NULL
			       ) AS remote_deleted
			FROM backup_upload_operations source
		) o;`,
		`DROP TABLE backup_upload_operations;`,
		`ALTER TABLE backup_upload_operations_v35 RENAME TO backup_upload_operations;`,
		`CREATE INDEX idx_backup_upload_operations_provider_status
			ON backup_upload_operations(provider_id, status, updated_at);`,
	}
}
