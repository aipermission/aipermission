package db

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var ErrDatabaseExists = errors.New("database name already exists")

const databaseDeleteQuarantinePrefix = ".aipermission-delete-"
const databaseDeleteCompleteMarker = ".complete"

var databaseRecoveryMu sync.Mutex

type DatabaseInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	State    string `json:"state"`
	Current  bool   `json:"current"`
	Unlocked bool   `json:"unlocked"`
}

func ListDatabases(defaultPath string, currentPath string) ([]DatabaseInfo, error) {
	databaseRecoveryMu.Lock()
	defer databaseRecoveryMu.Unlock()
	items := []DatabaseInfo{}
	if err := recoverDatabaseMoveJournals(filepath.Dir(defaultPath)); err != nil {
		return nil, err
	}
	if err := recoverDatabaseDeleteQuarantines(filepath.Dir(defaultPath)); err != nil {
		return nil, err
	}
	if Exists(defaultPath) {
		items = append(items, databaseInfo(DefaultDatabaseID(defaultPath), DefaultDatabaseName(defaultPath), defaultPath, currentPath))
	}

	dir := DatabasesDir(defaultPath)
	if err := recoverDatabaseMoveJournals(dir); err != nil {
		return nil, err
	}
	if err := recoverDatabaseDeleteQuarantines(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return items, nil
		}
		return nil, fmt.Errorf("list databases: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".db")
		path := filepath.Join(dir, entry.Name())
		items = append(items, databaseInfo(id, displayDatabaseName(id), path, currentPath))
	}
	return items, nil
}

func DatabasePath(defaultPath string, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "local-default" {
		return defaultPath, nil
	}
	if !validDatabaseID(id) {
		return "", fmt.Errorf("invalid database id")
	}
	namedPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
	if id == "default" && !Exists(namedPath) {
		return defaultPath, nil
	}
	return namedPath, nil
}

func DefaultDatabaseID(defaultPath string) string {
	if Exists(filepath.Join(DatabasesDir(defaultPath), "default.db")) {
		return "local-default"
	}
	return "default"
}

func DefaultDatabaseName(defaultPath string) string {
	if DefaultDatabaseID(defaultPath) == "local-default" {
		return "Local Default"
	}
	return "Default"
}

func NewDatabasePath(defaultPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	path := filepath.Join(dir, id+".db")
	for i := 2; Exists(path); i++ {
		nextID := fmt.Sprintf("%s-%d", id, i)
		path = filepath.Join(dir, nextID+".db")
		if !Exists(path) {
			id = nextID
			break
		}
	}
	return id, path, nil
}

func NewDatabasePathExact(defaultPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		return "", "", err
	}
	for _, item := range items {
		if slugifyDatabaseName(item.Name) == id {
			return "", "", ErrDatabaseExists
		}
	}
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	path := filepath.Join(dir, id+".db")
	if Exists(path) {
		return "", "", ErrDatabaseExists
	}
	return id, path, nil
}

func RenameDatabase(defaultPath string, currentPath string, name string) (string, string, error) {
	id, targetPath, err := RenameDatabaseTarget(defaultPath, currentPath, name)
	if err != nil {
		return "", "", err
	}
	if err := MoveDatabase(currentPath, targetPath); err != nil {
		return "", "", err
	}
	return id, targetPath, nil
}

func RenameDatabaseTarget(defaultPath string, currentPath string, name string) (string, string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", "", fmt.Errorf("database name is required")
	}
	targetPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
	if currentPath == targetPath {
		return "", "", fmt.Errorf("database already has this name")
	}
	if Exists(targetPath) {
		return "", "", fmt.Errorf("database name already exists")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return "", "", fmt.Errorf("create databases directory: %w", err)
	}
	return id, targetPath, nil
}

func MoveDatabase(currentPath string, targetPath string) error {
	databaseRecoveryMu.Lock()
	defer databaseRecoveryMu.Unlock()
	if err := recoverDatabaseMoveJournals(databaseMoveRoot(currentPath, targetPath)); err != nil {
		return err
	}
	ownerships, err := acquireDatabaseOwnershipSet(currentPath, targetPath)
	if err != nil {
		return err
	}
	defer closeDatabaseOwnershipSet(ownerships)
	return moveDatabaseWithOps(currentPath, targetPath, defaultDatabaseMoveOps())
}

func DeleteDatabase(path string) error {
	databaseRecoveryMu.Lock()
	defer databaseRecoveryMu.Unlock()
	ownership, err := AcquireDatabaseOwnership(path)
	if err != nil {
		return err
	}
	defer ownership.Close()
	return deleteDatabaseWithOps(path, defaultDatabaseDeleteOps())
}

type quarantinedDatabaseFile struct {
	original   string
	quarantine string
}

type databaseDeleteOps struct {
	lstat     func(string) (os.FileInfo, error)
	glob      func(string) ([]string, error)
	mkdir     func(string, os.FileMode) error
	rename    func(string, string) error
	write     func(string, []byte, os.FileMode) error
	syncFile  func(string) error
	syncDir   func(string) error
	remove    func(string) error
	removeAll func(string) error
}

func defaultDatabaseDeleteOps() databaseDeleteOps {
	return databaseDeleteOps{
		lstat: os.Lstat, glob: filepath.Glob, mkdir: os.Mkdir, rename: os.Rename,
		write: os.WriteFile, syncFile: syncDatabaseDeletePath,
		syncDir: syncDatabaseDeletePath, remove: os.Remove, removeAll: os.RemoveAll,
	}
}

func deleteDatabaseWithOps(path string, ops databaseDeleteOps) error {
	candidates := []string{path, path + "-wal", path + "-shm", path + "-journal"}
	for _, pattern := range []string{path + ".pre-migration-v*.aipdb", path + ".pre-migration-v*.aipdb.pending"} {
		matches, err := ops.glob(pattern)
		if err != nil {
			return fmt.Errorf("inspect database recovery artifacts: %w", err)
		}
		candidates = append(candidates, matches...)
	}
	existing := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := ops.lstat(candidate); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect database file %q: %w", candidate, err)
		}
		existing = append(existing, candidate)
	}
	if len(existing) == 0 {
		return nil
	}

	suffix, err := databaseDeleteQuarantineSuffix()
	if err != nil {
		return err
	}
	quarantineDir := filepath.Join(filepath.Dir(path), databaseDeleteQuarantinePrefix+suffix)
	if err := ops.mkdir(quarantineDir, 0o700); err != nil {
		return fmt.Errorf("create database delete quarantine: %w", err)
	}
	parentDir := filepath.Dir(path)
	if err := ops.syncDir(parentDir); err != nil {
		_ = ops.removeAll(quarantineDir)
		return fmt.Errorf("sync database delete quarantine creation: %w", err)
	}
	moved := make([]quarantinedDatabaseFile, 0, len(existing))
	markerPath := filepath.Join(quarantineDir, databaseDeleteCompleteMarker)
	rollback := func(cause error) error {
		rollbackErrors := []error{cause}
		if err := ops.remove(markerPath); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove database delete marker: %w", err))
			return errors.Join(rollbackErrors...)
		}
		if err := ops.syncDir(quarantineDir); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("sync removed database delete marker: %w", err))
			return errors.Join(rollbackErrors...)
		}
		rollbackComplete := true
		for movedIndex := len(moved) - 1; movedIndex >= 0; movedIndex-- {
			item := moved[movedIndex]
			if rollbackErr := ops.rename(item.quarantine, item.original); rollbackErr != nil {
				rollbackComplete = false
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore database file %q: %w", item.original, rollbackErr))
			}
		}
		if rollbackComplete {
			for _, item := range moved {
				if syncErr := ops.syncFile(item.original); syncErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("sync restored database file %q: %w", item.original, syncErr))
				}
			}
			if syncErr := ops.syncDir(parentDir); syncErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("sync restored database directory: %w", syncErr))
			}
			if removeErr := ops.removeAll(quarantineDir); removeErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove rolled back database quarantine: %w", removeErr))
			} else if syncErr := ops.syncDir(parentDir); syncErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("sync removed database quarantine: %w", syncErr))
			}
		} else {
			if syncErr := ops.syncDir(quarantineDir); syncErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("sync retained database quarantine: %w", syncErr))
			}
			if syncErr := ops.syncDir(parentDir); syncErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("sync database parent after failed rollback: %w", syncErr))
			}
		}
		return errors.Join(rollbackErrors...)
	}
	for _, candidate := range existing {
		quarantine := filepath.Join(quarantineDir, filepath.Base(candidate))
		if err := ops.rename(candidate, quarantine); err != nil {
			return rollback(fmt.Errorf("quarantine database file %q: %w", candidate, err))
		}
		moved = append(moved, quarantinedDatabaseFile{original: candidate, quarantine: quarantine})
	}
	for _, item := range moved {
		if err := ops.syncFile(item.quarantine); err != nil {
			return rollback(fmt.Errorf("sync quarantined database file %q: %w", item.quarantine, err))
		}
	}
	if err := ops.syncDir(quarantineDir); err != nil {
		return rollback(fmt.Errorf("sync database delete quarantine: %w", err))
	}
	if err := ops.syncDir(parentDir); err != nil {
		return rollback(fmt.Errorf("sync database parent directory: %w", err))
	}
	if err := ops.write(markerPath, []byte("complete\n"), 0o600); err != nil {
		return rollback(fmt.Errorf("complete database delete quarantine: %w", err))
	}
	if err := ops.syncFile(markerPath); err != nil {
		return rollback(fmt.Errorf("sync database delete marker: %w", err))
	}
	if err := ops.syncDir(quarantineDir); err != nil {
		return rollback(fmt.Errorf("sync completed database delete quarantine: %w", err))
	}
	if err := ops.syncDir(parentDir); err != nil {
		return rollback(fmt.Errorf("sync completed database parent directory: %w", err))
	}

	// Once every file is quarantined, the logical database deletion is complete.
	// Failed physical cleanup remains hidden and is retried during catalog reads.
	if ops.removeAll(quarantineDir) == nil {
		_ = ops.syncDir(parentDir)
	}
	return nil
}

func syncDatabaseDeletePath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func databaseDeleteQuarantineSuffix() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create database delete quarantine id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func recoverDatabaseDeleteQuarantines(dir string) error {
	return recoverDatabaseDeleteQuarantinesWithPublish(dir, PublishFileNoReplace)
}

func recoverDatabaseDeleteQuarantinesWithPublish(dir string, publish func(string, string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect database delete quarantines: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), databaseDeleteQuarantinePrefix) {
			continue
		}
		quarantineDir := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(quarantineDir, databaseDeleteCompleteMarker)); err == nil {
			_ = os.RemoveAll(quarantineDir)
			continue
		}
		files, err := os.ReadDir(quarantineDir)
		if err != nil {
			return fmt.Errorf("inspect incomplete database delete quarantine: %w", err)
		}
		restoreCandidates := make([]quarantinedDatabaseFile, 0, len(files))
		var databasePath string
		for _, file := range files {
			if file.IsDir() || file.Name() == databaseDeleteCompleteMarker {
				continue
			}
			original := filepath.Join(dir, file.Name())
			if databasePath == "" && filepath.Ext(file.Name()) == ".db" {
				databasePath = original
			}
			restoreCandidates = append(restoreCandidates, quarantinedDatabaseFile{
				original: original, quarantine: filepath.Join(quarantineDir, file.Name()),
			})
		}
		if databasePath == "" {
			return fmt.Errorf("incomplete database delete quarantine has no database file")
		}
		ownership, err := AcquireDatabaseOwnership(databasePath)
		if errors.Is(err, ErrDatabaseInUse) {
			continue
		}
		if err != nil {
			return fmt.Errorf("claim incomplete database delete quarantine: %w", err)
		}
		conflicted := false
		for _, item := range restoreCandidates {
			if _, err := os.Lstat(item.original); err == nil || !os.IsNotExist(err) {
				conflicted = true
				break
			}
		}
		if conflicted {
			_ = ownership.Close()
			continue
		}
		restored := make([]quarantinedDatabaseFile, 0, len(restoreCandidates))
		for _, item := range restoreCandidates {
			if err := publish(item.quarantine, item.original); err != nil {
				rollbackErrors := []error{fmt.Errorf("recover database file %q: %w", item.original, err)}
				for index := len(restored) - 1; index >= 0; index-- {
					restoredItem := restored[index]
					if rollbackErr := publish(restoredItem.original, restoredItem.quarantine); rollbackErr != nil {
						rollbackErrors = append(rollbackErrors, fmt.Errorf("roll back recovered database file %q: %w", restoredItem.original, rollbackErr))
					}
				}
				_ = ownership.Close()
				return errors.Join(rollbackErrors...)
			}
			restored = append(restored, item)
		}
		for _, item := range restoreCandidates {
			if err := syncDatabaseDeletePath(item.original); err != nil {
				_ = ownership.Close()
				return fmt.Errorf("sync recovered database file %q: %w", item.original, err)
			}
		}
		if err := syncDatabaseDeletePath(quarantineDir); err != nil {
			_ = ownership.Close()
			return fmt.Errorf("sync recovered database quarantine: %w", err)
		}
		if err := syncDatabaseDeletePath(dir); err != nil {
			_ = ownership.Close()
			return fmt.Errorf("sync recovered database directory: %w", err)
		}
		if err := os.Remove(quarantineDir); err != nil {
			_ = ownership.Close()
			return fmt.Errorf("remove recovered database delete quarantine: %w", err)
		}
		if err := syncDatabaseDeletePath(dir); err != nil {
			_ = ownership.Close()
			return fmt.Errorf("sync removed database delete quarantine: %w", err)
		}
		_ = ownership.Close()
	}
	return nil
}

func DatabasesDir(defaultPath string) string {
	return filepath.Join(filepath.Dir(defaultPath), "databases")
}

func databaseInfo(id string, name string, path string, currentPath string) DatabaseInfo {
	state := "locked"
	if LooksLikePlainSQLite(path) {
		state = "unsupported_plaintext"
	}
	return DatabaseInfo{
		ID:      id,
		Name:    name,
		Path:    path,
		State:   state,
		Current: path == currentPath,
	}
}

func displayDatabaseName(id string) string {
	if id == "default" {
		return "Default"
	}
	return strings.ReplaceAll(id, "-", " ")
}

var databaseIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validDatabaseID(id string) bool {
	return databaseIDPattern.MatchString(id)
}

func slugifyDatabaseName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	builder := strings.Builder{}
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
