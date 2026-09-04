package db

var connectorActionIdempotencyTombstoneMigration = migration{
	version:     25,
	description: "retain connector action idempotency tombstones",
	statements: []string{
		`CREATE TABLE IF NOT EXISTS connector_action_idempotency_tombstones (
			idempotency_scope TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			idempotency_identity_hash TEXT NOT NULL,
			request_id INTEGER NOT NULL,
			token_id INTEGER,
			target_id INTEGER NOT NULL,
			target_name TEXT NOT NULL,
			profile_id INTEGER NOT NULL,
			profile_label TEXT NOT NULL,
			connector_kind TEXT NOT NULL,
			action_name TEXT NOT NULL,
			source TEXT NOT NULL,
			retry_policy_json TEXT NOT NULL,
			status TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			retained_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			PRIMARY KEY (idempotency_scope, idempotency_key)
		) WITHOUT ROWID;`,
		`DROP TRIGGER IF EXISTS retain_connector_action_idempotency_tombstone;`,
		`CREATE TRIGGER retain_connector_action_idempotency_tombstone
		 BEFORE DELETE ON connector_action_requests
		 WHEN OLD.idempotency_key <> '' AND OLD.completed_at IS NOT NULL
		 BEGIN
			INSERT INTO connector_action_idempotency_tombstones (
				idempotency_scope, idempotency_key, idempotency_identity_hash,
				request_id, token_id, target_id, target_name, profile_id, profile_label,
				connector_kind, action_name, source, retry_policy_json,
				status, completed_at, retained_at, expires_at
			) VALUES (
				OLD.idempotency_scope, OLD.idempotency_key, OLD.idempotency_identity_hash,
				OLD.id, OLD.token_id, OLD.target_id,
				COALESCE((SELECT name FROM connector_targets WHERE id = OLD.target_id), ''),
				OLD.profile_id,
				COALESCE((SELECT label FROM connector_credential_profiles WHERE id = OLD.profile_id), ''),
				OLD.connector_kind, OLD.action_name, OLD.source, OLD.retry_policy_json,
				OLD.status, OLD.completed_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
				strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+30 days')
			) ON CONFLICT(idempotency_scope, idempotency_key) DO UPDATE SET
				idempotency_identity_hash = excluded.idempotency_identity_hash,
				request_id = excluded.request_id,
				token_id = excluded.token_id,
				target_id = excluded.target_id,
				target_name = excluded.target_name,
				profile_id = excluded.profile_id,
				profile_label = excluded.profile_label,
				connector_kind = excluded.connector_kind,
				action_name = excluded.action_name,
				source = excluded.source,
				retry_policy_json = excluded.retry_policy_json,
				status = excluded.status,
				completed_at = excluded.completed_at,
				retained_at = excluded.retained_at,
				expires_at = excluded.expires_at;
		 END;`,
	},
}
