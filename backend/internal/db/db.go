package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

var ErrPublishTargetExists = errors.New("publish target already exists")

const (
	currentSchemaVersion     = 33
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

// OpenEncryptedImportCandidate upgrades a disposable import copy; installed databases must use the recoverable path.
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
	if err := preparePrivateDatabasePath(path); err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Set("_foreign_keys", "ON")
	if password != "" {
		values.Set("_key", quoteSQLDoubleQuotedString(password))
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}
	uriPath := filepath.ToSlash(absolutePath)
	if runtime.GOOS == "windows" && filepath.VolumeName(absolutePath) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: values.Encode()}).String()

	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := protectDatabaseFiles(path); err != nil {
		_ = database.Close()
		return nil, err
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

func preparePrivateDatabasePath(path string) error {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		info, err = os.Lstat(directory)
		if err == nil {
			err = os.Chmod(directory, 0o700)
		}
	}
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data directory must be a regular directory: %s", directory)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("data directory must not be writable by group or other users: %s", directory)
	}
	if err := protectExistingDatabaseFile(path); os.IsNotExist(err) {
		created, createErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return fmt.Errorf("create private database file: %w", createErr)
		}
		if closeErr := created.Close(); closeErr != nil {
			return fmt.Errorf("close private database file: %w", closeErr)
		}
	} else if err != nil {
		return err
	}
	return protectDatabaseSidecars(path)
}

func protectDatabaseFiles(path string) error {
	if err := protectExistingDatabaseFile(path); err != nil {
		return err
	}
	return protectDatabaseSidecars(path)
}

func protectDatabaseSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := protectExistingDatabaseFile(path + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func protectExistingDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("database path must be a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open database file for permission check: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		return fmt.Errorf("database file changed during permission check: %s", path)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("protect database file: %w", err)
	}
	return nil
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
	if version < 1 || version > currentSchemaVersion {
		return "", nil
	}
	needsMigration := false
	for _, migration := range migrations() {
		applied, err := migrationApplied(database, migration.version)
		if err != nil {
			return "", fmt.Errorf("inspect migrations before snapshot: %w", err)
		}
		if !applied {
			needsMigration = true
			break
		}
	}
	if !needsMigration {
		return "", nil
	}
	targetPath := databasePath + ".pre-migration-v" + strconv.Itoa(version) + ".aipdb"
	if err := replacePreMigrationSnapshot(database, targetPath); err != nil {
		return "", fmt.Errorf("create encrypted pre-migration snapshot: %w", err)
	}
	return targetPath, nil
}

func replacePreMigrationSnapshot(database *sql.DB, targetPath string) error {
	pendingPath := targetPath + ".pending"
	if err := Snapshot(database, pendingPath); err != nil {
		return err
	}
	if err := PublishFile(pendingPath, targetPath); err != nil {
		_ = os.Remove(pendingPath)
		return fmt.Errorf("publish pre-migration snapshot: %w", err)
	}
	return nil
}

// PublishFile durably replaces targetPath and syncs the source and parent directory.
func PublishFile(sourcePath string, targetPath string) error {
	return publishFile(sourcePath, targetPath, true)
}

// PublishFileNoReplace uses an atomic hard-link boundary and never replaces an existing target.
func PublishFileNoReplace(sourcePath string, targetPath string) error {
	return publishFile(sourcePath, targetPath, false)
}

func publishFile(sourcePath string, targetPath string, replace bool) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open file for durable publish: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync file for durable publish: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file for durable publish: %w", err)
	}
	if replace {
		if err := os.Rename(sourcePath, targetPath); err != nil {
			return fmt.Errorf("rename published file: %w", err)
		}
	} else {
		if err := os.Link(sourcePath, targetPath); err != nil {
			if errors.Is(err, os.ErrExist) {
				return ErrPublishTargetExists
			}
			return fmt.Errorf("link published file: %w", err)
		}
		if err := os.Remove(sourcePath); err != nil {
			if rollbackErr := removeOwnedPublishedLink(sourcePath, targetPath); rollbackErr != nil {
				return errors.Join(fmt.Errorf("remove published source: %w", err), rollbackErr)
			}
			return fmt.Errorf("remove published source: %w", err)
		}
	}
	directories := []string{filepath.Dir(targetPath)}
	if sourceDirectory := filepath.Dir(sourcePath); sourceDirectory != directories[0] {
		directories = append(directories, sourceDirectory)
	}
	for _, directoryPath := range directories {
		directory, err := os.Open(directoryPath)
		if err != nil {
			return rollbackNoReplacePublication(sourcePath, targetPath, replace, fmt.Errorf("open published file directory: %w", err))
		}
		if err := directory.Sync(); err != nil {
			_ = directory.Close()
			return rollbackNoReplacePublication(sourcePath, targetPath, replace, fmt.Errorf("sync published file directory: %w", err))
		}
		if err := directory.Close(); err != nil {
			return rollbackNoReplacePublication(sourcePath, targetPath, replace, fmt.Errorf("close published file directory: %w", err))
		}
	}
	return nil
}

func rollbackNoReplacePublication(sourcePath, targetPath string, replace bool, cause error) error {
	if replace {
		return cause
	}
	if err := os.Link(targetPath, sourcePath); err != nil {
		return errors.Join(cause, fmt.Errorf("restore unpublished source: %w", err))
	}
	if err := os.Remove(targetPath); err != nil {
		return errors.Join(cause, fmt.Errorf("remove rolled back published target: %w", err))
	}
	directories := []string{filepath.Dir(sourcePath)}
	if targetDirectory := filepath.Dir(targetPath); targetDirectory != directories[0] {
		directories = append(directories, targetDirectory)
	}
	for _, directoryPath := range directories {
		directory, err := os.Open(directoryPath)
		if err == nil {
			err = directory.Sync()
			if closeErr := directory.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return errors.Join(cause, fmt.Errorf("sync publication rollback directory: %w", err))
		}
	}
	return cause
}

func removeOwnedPublishedLink(sourcePath, targetPath string) error {
	source, sourceErr := os.Stat(sourcePath)
	target, targetErr := os.Stat(targetPath)
	if sourceErr != nil || targetErr != nil || !os.SameFile(source, target) {
		return errors.New("published target ownership could not be verified")
	}
	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("remove partially published target: %w", err)
	}
	return nil
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
	return SnapshotContext(context.Background(), database, targetPath)
}

func SnapshotContext(ctx context.Context, database *sql.DB, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	_ = os.Remove(targetPath)
	if _, err := database.ExecContext(ctx, `VACUUM INTO ?`, targetPath); err != nil {
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
