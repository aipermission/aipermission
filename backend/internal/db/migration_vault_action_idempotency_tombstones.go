package db

var vaultActionIdempotencyTombstoneMigration = migration{
	version:     40,
	description: "retain Vault action idempotency tombstones",
	statements: []string{
		`CREATE TABLE IF NOT EXISTS vault_action_idempotency_tombstones (
			token_id INTEGER NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			retained_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			PRIMARY KEY (token_id, idempotency_key)
		) WITHOUT ROWID;`,
		`CREATE INDEX IF NOT EXISTS idx_vault_action_idempotency_tombstones_expiry
			ON vault_action_idempotency_tombstones(expires_at);`,
		`CREATE TRIGGER IF NOT EXISTS retain_vault_action_idempotency_tombstone
		 BEFORE DELETE ON vault_action_requests
		 WHEN OLD.idempotency_key <> '' AND OLD.completed_at IS NOT NULL
		 BEGIN
			INSERT INTO vault_action_idempotency_tombstones (
				token_id, idempotency_key, request_id, status,
				completed_at, retained_at, expires_at
			) VALUES (
				OLD.token_id, OLD.idempotency_key, OLD.id, OLD.status,
				OLD.completed_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
				strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+30 days')
			) ON CONFLICT(token_id, idempotency_key) DO UPDATE SET
				request_id = excluded.request_id,
				status = excluded.status,
				completed_at = excluded.completed_at,
				retained_at = excluded.retained_at,
				expires_at = excluded.expires_at;
		 END;`,
	},
}
