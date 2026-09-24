package db

var vaultFinalizationMigration = migration{
	version:     41,
	description: "durable Vault mutation finalization",
	statements: []string{
		`CREATE TABLE IF NOT EXISTS vault_finalizations (id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL CHECK (kind IN ('mutation', 'project')), item_id INTEGER NOT NULL DEFAULT 0, binding_id INTEGER NOT NULL DEFAULT 0, project_id INTEGER NOT NULL DEFAULT 0, references_json TEXT NOT NULL DEFAULT '[]', reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);`,
	},
}
