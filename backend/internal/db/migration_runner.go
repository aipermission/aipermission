package db

import (
	"database/sql"
	"fmt"
)

func migrate(database *sql.DB) error {
	if err := ensureMigrationTable(database); err != nil {
		return err
	}
	if err := rejectUnsupportedPreviewSchema(database); err != nil {
		return err
	}
	if err := runSchemaMigrations(database); err != nil {
		return err
	}
	if err := syncHistoryProjections(database); err != nil {
		return err
	}
	if err := runMigrationMaintenance(database); err != nil {
		return err
	}
	return nil
}

func ensureMigrationTable(database *sql.DB) error {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func rejectUnsupportedPreviewSchema(database *sql.DB) error {
	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		return fmt.Errorf("read schema migration count: %w", err)
	}
	if migrationCount == 0 {
		hasExistingSchema, err := hasPreexistingApplicationSchema(database)
		if err != nil {
			return err
		}
		if hasExistingSchema {
			return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
		}
		return nil
	}

	var baselineDescription string
	err := database.QueryRow(`SELECT description FROM schema_migrations WHERE version = ?`, connectorNativeBaselineVersion).Scan(&baselineDescription)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
	}
	if err != nil {
		return fmt.Errorf("read connector-native baseline migration: %w", err)
	}
	if baselineDescription != connectorNativeBaselineDescription {
		return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
	}

	var maxVersion int
	if err := database.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		return fmt.Errorf("read max schema migration: %w", err)
	}
	if maxVersion > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this gateway supports", maxVersion)
	}
	return nil
}

func hasPreexistingApplicationSchema(database *sql.DB) (bool, error) {
	var count int
	err := database.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name NOT IN ('schema_migrations')
			AND name NOT LIKE 'sqlite_%'
	`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect existing schema: %w", err)
	}
	return count > 0, nil
}

func runSchemaMigrations(database *sql.DB) error {
	for _, migration := range migrations {
		applied, err := migrationApplied(database, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := runSingleMigration(database, migration); err != nil {
			return err
		}
	}
	return nil
}

func migrationApplied(database *sql.DB, version int) (bool, error) {
	var exists int
	err := database.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("read schema migration %d: %w", version, err)
}

func runSingleMigration(database *sql.DB, migration migration) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", migration.version, err)
	}
	defer tx.Rollback()

	if migration.preflight != nil {
		if err := migration.preflight(tx); err != nil {
			return fmt.Errorf("preflight migration %d: %w", migration.version, err)
		}
	}
	for _, statement := range migration.statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("run migration %d: %w", migration.version, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, datetime('now'))`,
		migration.version,
		migration.description,
	); err != nil {
		return fmt.Errorf("record schema migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", migration.version, err)
	}
	return nil
}
