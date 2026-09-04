package db

var historyKeysetPaginationMigration = migration{
	version:     27,
	description: "history keyset pagination indexes",
	statements: []string{
		`DROP INDEX IF EXISTS idx_history_entries_created;`,
		`CREATE INDEX idx_history_entries_created ON history_entries(created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_kind_created;`,
		`CREATE INDEX idx_history_entries_kind_created ON history_entries(connector_kind, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_activity_created;`,
		`CREATE INDEX idx_history_entries_activity_created ON history_entries(activity_type, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_status_created;`,
		`CREATE INDEX idx_history_entries_status_created ON history_entries(status, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_target_created;`,
		`CREATE INDEX idx_history_entries_target_created ON history_entries(target_id, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_profile_created;`,
		`CREATE INDEX idx_history_entries_profile_created ON history_entries(profile_id, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_runtime_created;`,
		`CREATE INDEX idx_history_entries_runtime_created ON history_entries(runtime_id, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_source_created;`,
		`CREATE INDEX idx_history_entries_source_created ON history_entries(source, created_at DESC, id DESC);`,
		`DROP INDEX IF EXISTS idx_history_entries_project_created;`,
		`CREATE INDEX idx_history_entries_project_created ON history_entries(project_id, created_at DESC, id DESC);`,
	},
}
