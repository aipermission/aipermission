package db

import "database/sql"

var credentialSecretRevisionMigration = migration{
	version:     23,
	description: "add credential secret revision compare-and-swap",
	preflight:   ensureCredentialSecretRevision,
}

func ensureCredentialSecretRevision(tx *sql.Tx) error {
	return ensureColumn(
		tx,
		"connector_credential_profiles",
		"secret_revision",
		`ALTER TABLE connector_credential_profiles ADD COLUMN secret_revision INTEGER NOT NULL DEFAULT 1`,
	)
}
