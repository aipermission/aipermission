package db

import (
	"database/sql"
	"fmt"
)

// recordEnvelopeBoundaryMigration makes the record-bound encryption format a
// database compatibility boundary. The encrypted payload rewrite runs after
// schema migrations, once the database-scoped Vault is available.
var recordEnvelopeBoundaryMigration = migration{
	version:     19,
	description: "record-bound encrypted payload envelopes",
	preflight: func(tx *sql.Tx) error {
		var partialSessionHandles int
		if err := tx.QueryRow(`
			SELECT COUNT(*)
			FROM connector_action_requests
			WHERE (session_id IS NULL) <> (session_generation IS NULL)
		`).Scan(&partialSessionHandles); err != nil {
			return fmt.Errorf("validate connector action session handles: %w", err)
		}
		if partialSessionHandles > 0 {
			return fmt.Errorf("connector_action_requests contains %d partial session handle(s); session_id and session_generation must both be null or both be set", partialSessionHandles)
		}
		return nil
	},
	statements: []string{
		`ALTER TABLE connector_action_requests ADD COLUMN retry_policy_json TEXT NOT NULL DEFAULT '{"class":"non_idempotent","guidance":"Inspect external state before retrying."}';`,
		`ALTER TABLE history_entries ADD COLUMN retry_policy_json TEXT NOT NULL DEFAULT '{}';`,
		`UPDATE history_entries
		 SET retry_policy_json = '{"class":"non_idempotent","guidance":"Inspect external state before retrying."}'
		 WHERE source_ref_type = 'connector_action_request';`,
		`CREATE TRIGGER IF NOT EXISTS connector_action_requests_session_pair_insert
		 BEFORE INSERT ON connector_action_requests
		 WHEN (NEW.session_id IS NULL) <> (NEW.session_generation IS NULL)
		 BEGIN
			SELECT RAISE(ABORT, 'session_id and session_generation must both be null or both be set');
		 END;`,
		`CREATE TRIGGER IF NOT EXISTS connector_action_requests_session_pair_update
		 BEFORE UPDATE OF session_id, session_generation ON connector_action_requests
		 WHEN (NEW.session_id IS NULL) <> (NEW.session_generation IS NULL)
		 BEGIN
			SELECT RAISE(ABORT, 'session_id and session_generation must both be null or both be set');
		 END;`,
	},
}
