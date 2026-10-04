// Package baselineschema owns the released table and index definitions for
// initial connector, backup and Vault storage. It performs no database I/O;
// migration ordering, transactions, upgrade policy and recovery belong to db.
package baselineschema

import "slices"

// Connector returns the ordered statements for the connector-native baseline.
// Each call owns its slice, so callers cannot mutate another migration plan.
func Connector() []string {
	return slices.Concat(coreTableStatements, fileTransferTableStatements, historyTableStatements, indexStatements, searchIndexStatements)
}

// BackupProviders returns independently owned backup metadata definitions.
func BackupProviders() []string {
	return slices.Concat(backupProviderTableStatements, backupProviderIndexStatements)
}

// ProjectVault returns independently owned project Vault definitions.
func ProjectVault() []string {
	return slices.Clone(projectVaultTableStatements)
}

var backupProviderIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_backup_providers_type_status ON backup_providers(provider_type, status);`,
	`CREATE INDEX IF NOT EXISTS idx_backup_records_provider_database_time ON backup_records(provider_id, database_name, backup_created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_backup_records_database_time ON backup_records(database_name, backup_created_at);`,
}
