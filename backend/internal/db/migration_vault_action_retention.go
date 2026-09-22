package db

var vaultActionRetentionIndexMigration = migration{
	version:     39,
	description: "Vault action request retention index",
	statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_vault_action_requests_retention_completed
			ON vault_action_requests(julianday(completed_at)) WHERE completed_at IS NOT NULL;`,
	},
}
