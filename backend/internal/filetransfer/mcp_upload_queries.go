package filetransfer

const mcpUploadStatusSQL = `SELECT i.resource_id, COALESCE(a.id, 0)
	FROM file_transfer_start_idempotency i
	LEFT JOIN connector_action_requests a ON a.token_id = ? AND a.source = 'mcp'
		AND a.idempotency_scope = ? AND a.idempotency_key <> ''
		AND a.idempotency_key = i.idempotency_key AND a.connector_kind = ? AND a.target_id = ?
		AND a.profile_id = ? AND a.action_name = ?
	WHERE i.scope = ? AND i.idempotency_key = ? AND i.resource_kind = ?`

const mcpUploadDisposableStageSQL = `EXISTS (SELECT 1 FROM file_transfer_start_idempotency i
	WHERE i.resource_id = f.id AND i.resource_kind = 'transfer' AND i.scope LIKE 'mcp-upload:%'
	AND (f.temp_expires_at <= ? OR EXISTS (
		SELECT 1 FROM connector_action_requests a
		WHERE a.token_id = CAST(substr(i.scope, length('mcp-upload:') + 1) AS INTEGER)
		AND a.idempotency_scope = ? || substr(i.scope, length('mcp-upload:') + 1)
		AND a.idempotency_key <> ''
		AND a.source = 'mcp' AND a.idempotency_key = i.idempotency_key
		AND a.status IN ('declined', 'stale', 'blocked', 'failed', 'error', 'canceled')
	)))`
