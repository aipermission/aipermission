package actioncapacity

import "strings"

var measuredColumns = []string{
	"title", "summary", "preview_json", "source", "input_json", "encrypted_payload_json",
	"reason", "status", "output_json", "display_text", "error", "approval_context",
	"approval_context_hash", "approval_context_drift", "retry_policy_json", "idempotency_key",
	"idempotency_identity_hash", "idempotency_scope", "execution_owner", "execution_lease_expires_at",
	"dispatch_started_at", "created_at", "completed_at",
}

// RecordBytesSQL is shared by backfill, write triggers and verification queries.
// The prefix is a trusted SQL row alias, never external input.
func RecordBytesSQL(prefix string) string {
	terms := make([]string, len(measuredColumns))
	for index, column := range measuredColumns {
		terms[index] = "LENGTH(CAST(COALESCE(" + prefix + column + ", '') AS BLOB))"
	}
	return strings.Join(terms, " + ")
}

// ProjectionStatements maintains compact measurements atomically with canonical
// request writes, including recovery, retention and token deletion.
func ProjectionStatements() []string {
	return []string{
		`CREATE TABLE connector_action_request_usage (
			request_id INTEGER PRIMARY KEY REFERENCES connector_action_requests(id) ON DELETE CASCADE,
			token_id INTEGER REFERENCES api_tokens(id) ON DELETE CASCADE,
			stored_bytes INTEGER NOT NULL CHECK(stored_bytes >= 0),
			status TEXT NOT NULL
		);`,
		`CREATE INDEX idx_connector_action_request_usage_token ON connector_action_request_usage(token_id);`,
		`INSERT INTO connector_action_request_usage(request_id, token_id, stored_bytes, status)
		 SELECT id, token_id, ` + RecordBytesSQL("") + `, status FROM connector_action_requests;`,
		`CREATE TRIGGER project_connector_action_usage_insert AFTER INSERT ON connector_action_requests
		 BEGIN INSERT INTO connector_action_request_usage(request_id, token_id, stored_bytes, status)
		 VALUES(NEW.id, NEW.token_id, ` + RecordBytesSQL("NEW.") + `, NEW.status); END;`,
		`CREATE TRIGGER project_connector_action_usage_update AFTER UPDATE ON connector_action_requests
		 BEGIN INSERT INTO connector_action_request_usage(request_id, token_id, stored_bytes, status)
		 VALUES(NEW.id, NEW.token_id, ` + RecordBytesSQL("NEW.") + `, NEW.status)
		 ON CONFLICT(request_id) DO UPDATE SET token_id=excluded.token_id,
		 stored_bytes=excluded.stored_bytes, status=excluded.status; END;`,
	}
}
