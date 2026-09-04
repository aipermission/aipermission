package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

const (
	currentSchemaVersion     = 22
	expectedSQLCipherVersion = "4.16.0"
	expectedSQLiteVersion    = "3.53.1"
	expectedKDFIterations    = 256000
)

// CurrentSchemaVersion returns the newest schema understood by this build.
func CurrentSchemaVersion() int {
	return currentSchemaVersion
}

func OpenEncrypted(path string, password string) (*sql.DB, error) {
	return openEncrypted(path, password, openOptions{runMigrations: true, createMigrationSnapshot: true})
}

func OpenEncryptedForMigration(path string, password string) (*sql.DB, error) {
	return openEncrypted(path, password, openOptions{})
}

// OpenEncryptedImportCandidate upgrades a disposable import copy without
// creating a sibling pre-migration snapshot. The caller must not use this for
// an installed database: failed imports are discarded instead of recovered.
func OpenEncryptedImportCandidate(path string, password string) (*sql.DB, error) {
	return openEncrypted(path, password, openOptions{runMigrations: true})
}

func ValidateEncrypted(path string, password string) error {
	database, err := openEncrypted(path, password, openOptions{})
	if err != nil {
		return err
	}
	defer database.Close()

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&count); err != nil {
		return fmt.Errorf("verify encrypted sqlite: %w", err)
	}
	return nil
}

type openOptions struct {
	runMigrations           bool
	createMigrationSnapshot bool
}

func openEncrypted(path string, password string, options openOptions) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	values := url.Values{}
	values.Set("_foreign_keys", "ON")
	if password != "" {
		values.Set("_key", quoteSQLDoubleQuotedString(password))
	}

	dsn := path
	if encoded := values.Encode(); encoded != "" {
		dsn += "?" + encoded
	}

	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if options.runMigrations {
		snapshotPath := ""
		if options.createMigrationSnapshot {
			var err error
			snapshotPath, err = createPreMigrationSnapshot(database, path)
			if err != nil {
				_ = database.Close()
				return nil, err
			}
		}
		if err := migrate(database); err != nil {
			_ = database.Close()
			if snapshotPath != "" {
				return nil, fmt.Errorf("%w; encrypted pre-migration snapshot retained at %s", err, snapshotPath)
			}
			return nil, err
		}
	}

	return database, nil
}

func createPreMigrationSnapshot(database *sql.DB, databasePath string) (string, error) {
	var migrationTableExists int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&migrationTableExists); err != nil {
		return "", fmt.Errorf("inspect migration metadata: %w", err)
	}
	if migrationTableExists == 0 {
		return "", nil
	}
	var version int
	if err := database.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return "", fmt.Errorf("read schema version before migration: %w", err)
	}
	if version < 1 || version >= currentSchemaVersion {
		return "", nil
	}
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	targetPath := databasePath + ".pre-migration-v" + strconv.Itoa(version) + "-" + timestamp + ".aipdb"
	if err := Snapshot(database, targetPath); err != nil {
		return "", fmt.Errorf("create encrypted pre-migration snapshot: %w", err)
	}
	return targetPath, nil
}

func LooksLikePlainSQLite(path string) bool {
	header := make([]byte, 16)
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	n, _ := file.Read(header)
	return n == len(header) && string(header) == "SQLite format 3\x00"
}

func Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func Rekey(database *sql.DB, newPassword string) error {
	// SQLCipher PRAGMA rekey does not support parameter binding through this
	// driver. Escape double quotes because the driver and SQLCipher examples use
	// double-quoted PRAGMA key/rekey passphrases.
	if _, err := database.Exec(`PRAGMA rekey = "` + quoteSQLDoubleQuotedString(newPassword) + `"`); err != nil {
		return fmt.Errorf("rekey encrypted sqlite: %w", err)
	}
	if err := database.Ping(); err != nil {
		return fmt.Errorf("ping rekeyed sqlite: %w", err)
	}
	return nil
}

func Snapshot(database *sql.DB, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	_ = os.Remove(targetPath)
	if _, err := database.Exec(`VACUUM INTO ?`, targetPath); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("snapshot encrypted sqlite: %w", err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("chmod sqlite snapshot: %w", err)
	}
	return nil
}

func quoteSQLDoubleQuotedString(value string) string {
	return strings.ReplaceAll(value, `"`, `""`)
}
