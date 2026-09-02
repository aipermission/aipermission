package db

var retentionIndexMigration = migration{
	version:     18,
	description: "retention cleanup indexes",
	statements: []string{
		`CREATE INDEX idx_command_requests_retention_completed
			ON command_requests(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
		`CREATE INDEX idx_connector_action_requests_retention_completed
			ON connector_action_requests(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
		`CREATE INDEX idx_file_transfer_batches_retention_completed
			ON file_transfer_batches(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
		`CREATE INDEX idx_history_entries_retention_completed
			ON history_entries(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
		`CREATE INDEX idx_file_transfers_retention_completed
			ON file_transfers(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
		`CREATE INDEX idx_audit_logs_retention_created
			ON audit_logs(julianday(created_at));`,
		`CREATE INDEX idx_audit_outbox_retention_delivered
			ON audit_outbox(julianday(delivered_at)) WHERE delivered_at IS NOT NULL;`,
		`CREATE INDEX idx_audit_outbox_retention_dead_lettered
			ON audit_outbox(julianday(dead_lettered_at)) WHERE dead_lettered_at IS NOT NULL;`,
		`CREATE INDEX idx_console_sessions_retention_closed
			ON console_sessions(julianday(closed_at)) WHERE closed_at IS NOT NULL;`,
		`CREATE INDEX idx_message_queue_retention_consumed
			ON message_queue(julianday(consumed_at)) WHERE consumed_at IS NOT NULL;`,
	},
}
