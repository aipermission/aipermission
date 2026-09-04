package db

var actionApprovalIntegrityMigration = migration{
	version:     22,
	description: "enforce pending connector action approval integrity",
	statements: []string{
		`UPDATE connector_action_requests
		 SET status = 'stale',
		     error = 'connector approval integrity data is missing; ask the AI to send a fresh request',
		     approval_context_drift = 'request_integrity',
		     completed_at = COALESCE(completed_at, datetime('now'))
		 WHERE status = 'approval_pending'
		   AND (trim(encrypted_payload_json) = '' OR trim(approval_context) = '' OR trim(approval_context_hash) = '')`,
		`UPDATE history_entries
		 SET status = 'stale',
		     error = 'connector approval integrity data is missing; ask the AI to send a fresh request',
		     completed_at = COALESCE(completed_at, datetime('now')),
		     updated_at = datetime('now')
		 WHERE source_ref_type = 'connector_action_request'
		   AND source_ref_id IN (
		       SELECT id FROM connector_action_requests
		       WHERE status = 'stale' AND approval_context_drift = 'request_integrity'
		   )`,
		`DROP TRIGGER IF EXISTS protect_connector_action_envelope_nonempty_update`,
		`CREATE TRIGGER protect_connector_action_envelope_nonempty_update
		 BEFORE UPDATE OF encrypted_payload_json ON connector_action_requests
		 WHEN trim(OLD.encrypted_payload_json) <> '' AND trim(NEW.encrypted_payload_json) = ''
		 BEGIN
		     SELECT RAISE(ABORT, 'connector action encrypted envelope cannot be cleared');
		 END;`,
		`DROP TRIGGER IF EXISTS protect_connector_action_approval_context_nonempty_update`,
		`CREATE TRIGGER protect_connector_action_approval_context_nonempty_update
		 BEFORE UPDATE OF approval_context ON connector_action_requests
		 WHEN trim(OLD.approval_context) <> '' AND trim(NEW.approval_context) = ''
		 BEGIN
		     SELECT RAISE(ABORT, 'connector action approval context cannot be cleared');
		 END;`,
		`DROP TRIGGER IF EXISTS protect_connector_action_approval_hash_nonempty_update`,
		`CREATE TRIGGER protect_connector_action_approval_hash_nonempty_update
		 BEFORE UPDATE OF approval_context_hash ON connector_action_requests
		 WHEN trim(OLD.approval_context_hash) <> '' AND trim(NEW.approval_context_hash) = ''
		 BEGIN
		     SELECT RAISE(ABORT, 'connector action approval hash cannot be cleared');
		 END;`,
		`DROP TRIGGER IF EXISTS protect_connector_action_execution_integrity_update`,
		`CREATE TRIGGER protect_connector_action_execution_integrity_update
		 BEFORE UPDATE OF status ON connector_action_requests
		 WHEN OLD.status = 'approval_pending' AND NEW.status = 'running'
		  AND (trim(NEW.encrypted_payload_json) = '' OR trim(NEW.approval_context) = '' OR trim(NEW.approval_context_hash) = '')
		 BEGIN
		     SELECT RAISE(ABORT, 'connector action approval integrity data is required');
		 END;`,
		`DROP TRIGGER IF EXISTS protect_connector_action_pending_integrity_update`,
		`CREATE TRIGGER protect_connector_action_pending_integrity_update
		 BEFORE UPDATE OF status ON connector_action_requests
		 WHEN NEW.status = 'approval_pending'
		  AND (trim(NEW.encrypted_payload_json) = '' OR trim(NEW.approval_context) = '' OR trim(NEW.approval_context_hash) = '')
		 BEGIN
		     SELECT RAISE(ABORT, 'connector action approval integrity data is required');
		 END;`,
	},
}
