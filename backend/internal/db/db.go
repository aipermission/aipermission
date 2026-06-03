package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

const currentSchemaVersion = 2

func OpenEncrypted(path string, password string) (*sql.DB, error) {
	return openEncrypted(path, password, true)
}

func ValidateEncrypted(path string, password string) error {
	database, err := openEncrypted(path, password, false)
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

func openEncrypted(path string, password string, runMigrations bool) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	values := url.Values{}
	values.Set("_pragma_foreign_keys", "ON")
	if password != "" {
		values.Set("_pragma_key", quoteSQLDoubleQuotedString(password))
		values.Set("_pragma_cipher_page_size", "4096")
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
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if runMigrations {
		if err := migrate(database); err != nil {
			_ = database.Close()
			return nil, err
		}
	}

	return database, nil
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
	if _, err := database.Exec(`VACUUM INTO '` + quoteSQLString(targetPath) + `'`); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("snapshot encrypted sqlite: %w", err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		_ = os.Remove(targetPath)
		return fmt.Errorf("chmod sqlite snapshot: %w", err)
	}
	return nil
}

func quoteSQLString(value string) string {
	return strings.ReplaceAll(value, `'`, `''`)
}

func quoteSQLDoubleQuotedString(value string) string {
	return strings.ReplaceAll(value, `"`, `""`)
}
