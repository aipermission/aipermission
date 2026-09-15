package db

func vaultActionRequestEnvelopeMigration() migration {
	return migration{version: 33, description: "sealed Vault action request metadata", statements: []string{
		`ALTER TABLE vault_action_requests ADD COLUMN encrypted_payload_json TEXT NOT NULL DEFAULT '';`,
		`UPDATE vault_action_requests
		 SET status = 'stale',
		     error = 'Vault action request predates the encrypted metadata boundary; submit a fresh request',
		     completed_at = COALESCE(completed_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		     updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE status IN ('approval_pending', 'running');`,
		`UPDATE vault_action_requests
		 SET input_json = '{}', reason = '[REDACTED LEGACY METADATA]'
		 WHERE trim(input_json) <> '{}' OR trim(reason) <> '';`,
		`UPDATE history_entries
		 SET input_json = '{}', summary = '[REDACTED LEGACY METADATA]',
		     status = COALESCE((SELECT CASE WHEN r.status = 'approval_pending' THEN 'pending_approval' ELSE r.status END
		                        FROM vault_action_requests r WHERE r.id = history_entries.source_ref_id), status),
		     error = COALESCE((SELECT r.error FROM vault_action_requests r WHERE r.id = history_entries.source_ref_id), error),
		     completed_at = COALESCE((SELECT r.completed_at FROM vault_action_requests r WHERE r.id = history_entries.source_ref_id), completed_at),
		     updated_at = COALESCE((SELECT r.updated_at FROM vault_action_requests r WHERE r.id = history_entries.source_ref_id), updated_at)
		 WHERE source_ref_type = 'vault_action_request';`,
		`CREATE TRIGGER guard_vault_action_request_envelope_insert
		 BEFORE INSERT ON vault_action_requests
		 WHEN NEW.encrypted_payload_json <> '' AND
		      CASE WHEN json_valid(NEW.encrypted_payload_json) THEN
		        COALESCE(json_extract(NEW.encrypted_payload_json, '$.version') <> 1, 1)
		        OR COALESCE(json_extract(NEW.encrypted_payload_json, '$.algorithm') <> 'AES-256-GCM', 1)
		        OR COALESCE(json_type(NEW.encrypted_payload_json, '$.nonce') <> 'text', 1)
		        OR COALESCE(json_type(NEW.encrypted_payload_json, '$.ciphertext') <> 'text', 1)
		      ELSE 1 END
		 BEGIN
		   SELECT RAISE(ABORT, 'record-bound encrypted envelope is required');
		 END;`,
		`CREATE TRIGGER guard_vault_action_request_envelope_update
		 BEFORE UPDATE OF encrypted_payload_json ON vault_action_requests
		 WHEN NEW.encrypted_payload_json <> '' AND
		      CASE WHEN json_valid(NEW.encrypted_payload_json) THEN
		        COALESCE(json_extract(NEW.encrypted_payload_json, '$.version') <> 1, 1)
		        OR COALESCE(json_extract(NEW.encrypted_payload_json, '$.algorithm') <> 'AES-256-GCM', 1)
		        OR COALESCE(json_type(NEW.encrypted_payload_json, '$.nonce') <> 'text', 1)
		        OR COALESCE(json_type(NEW.encrypted_payload_json, '$.ciphertext') <> 'text', 1)
		      ELSE 1 END
		 BEGIN
		   SELECT RAISE(ABORT, 'record-bound encrypted envelope is required');
		 END;`,
		`CREATE TRIGGER protect_vault_action_request_envelope_update
		 BEFORE UPDATE OF encrypted_payload_json ON vault_action_requests
		 WHEN OLD.encrypted_payload_json <> '' AND NEW.encrypted_payload_json <> OLD.encrypted_payload_json
		 BEGIN
		   SELECT RAISE(ABORT, 'Vault action request envelope is immutable');
		 END;`,
	}}
}
