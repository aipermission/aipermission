package db

import "database/sql"

func projectScopeRevisionMigration() migration {
	return migration{
		version:     34,
		description: "monotonic token project scope revisions",
		preflight:   ensureProjectScopeRevision,
	}
}

func ensureProjectScopeRevision(tx *sql.Tx) error {
	return ensureColumn(
		tx,
		"token_project_scopes",
		"revision",
		`ALTER TABLE token_project_scopes ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0)`,
	)
}
