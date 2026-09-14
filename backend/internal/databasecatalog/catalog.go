package databasecatalog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/db"
)

var ErrDatabaseExists = errors.New("database name already exists")
var errDatabaseDeleteRecoveryConflict = errors.New("database delete recovery has a conflicting artifact")

const databaseDeleteQuarantinePrefix = ".aipermission-delete-"
const databaseDeleteCompleteMarker = ".complete"
const databaseDeleteManifestFile = ".manifest.json"
const databaseDeleteManifestPendingFile = ".manifest.pending"

type databaseDeleteManifest struct {
	Version int      `json:"version"`
	Primary string   `json:"primary"`
	Files   []string `json:"files"`
}

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
	if _, err := checkedDatabaseDirectory(filepath.Dir(defaultPath)); err != nil {
		return nil, err
	}
	if err := recoverDatabaseMoveJournals(filepath.Dir(defaultPath)); err != nil {
		return nil, err
	}
	if err := recoverDatabaseDeleteQuarantines(filepath.Dir(defaultPath)); err != nil {
		return nil, err
	}
	if _, err := checkedDatabasePath(defaultPath); err != nil {
		return nil, err
	}
	if databaseCatalogFileExists(defaultPath) {
		items = append(items, databaseInfo(DefaultDatabaseID(defaultPath), DefaultDatabaseName(defaultPath), defaultPath, currentPath))
	}

	dir := DatabasesDir(defaultPath)
	if _, err := checkedDatabaseDirectory(dir); err != nil {
		return nil, err
	}
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
		if !databaseCatalogEntryIsRegular(entry) || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".db")
		path := filepath.Join(dir, entry.Name())
		items = append(items, databaseInfo(databaseReference(id), displayDatabaseName(id), path, currentPath))
	}
	return items, nil
}

func DatabasePath(defaultPath string, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "local-default" {
		return checkedDatabasePath(defaultPath)
	}
	legacyID, legacy, err := legacyDatabaseID(defaultPath, id)
	if err != nil {
		return "", err
	}
	if legacy {
		return checkedDatabasePath(filepath.Join(DatabasesDir(defaultPath), legacyID+".db"))
	}
	if !validDatabaseID(id) {
		if validLegacyDatabaseID(id) {
			legacyPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
			if databaseCatalogFileExists(legacyPath) {
				return checkedDatabasePath(legacyPath)
			}
		}
		return "", fmt.Errorf("invalid database id")
	}
	namedPath := filepath.Join(DatabasesDir(defaultPath), id+".db")
	if id == "default" && !databaseCatalogFileExists(namedPath) {
		return checkedDatabasePath(defaultPath)
	}
	return checkedDatabasePath(namedPath)
}

func checkedDatabasePath(path string) (string, error) {
	return checkedManagedPath(path, false)
}

func checkedDatabaseDirectory(path string) (string, error) {
	return checkedManagedPath(path, true)
}

func checkedManagedPath(path string, directory bool) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	volume := filepath.VolumeName(abs)
	current := volume + string(os.PathSeparator)
	relative := strings.TrimPrefix(abs, current)
	parts := strings.Split(relative, string(os.PathSeparator))
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, inspectErr := os.Lstat(current)
		if os.IsNotExist(inspectErr) {
			return path, nil
		}
		if inspectErr != nil {
			return "", fmt.Errorf("inspect database path: %w", inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("database path must not traverse symbolic links")
		}
		last := index == len(parts)-1
		if !last && !info.IsDir() {
			return "", fmt.Errorf("database path parent must be a directory")
		}
		if last && directory && !info.IsDir() {
			return "", fmt.Errorf("database directory path must be a directory")
		}
		if last && !directory && !info.Mode().IsRegular() {
			return "", fmt.Errorf("database path must be a regular file")
		}
	}
	return path, nil
}

func databaseCatalogFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func DefaultDatabaseID(defaultPath string) string {
	if databaseCatalogFileExists(defaultPath) && databaseCatalogFileExists(filepath.Join(DatabasesDir(defaultPath), "default.db")) {
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
	id, err := canonicalDatabaseID(name)
	if err != nil {
		return "", "", err
	}
	dir := DatabasesDir(defaultPath)
	if err := ensureDatabaseDirectory(dir); err != nil {
		return "", "", err
	}
	baseID := id
	for sequence := 2; ; sequence++ {
		path := filepath.Join(dir, id+".db")
		if !databaseIDExists(defaultPath, id, path) {
			return id, path, nil
		}
		id = suffixedDatabaseID(baseID, sequence)
	}
}

func NewDatabasePathExact(defaultPath string, name string) (string, string, error) {
	id, err := canonicalDatabaseID(name)
	if err != nil {
		return "", "", err
	}
	if _, err := ListDatabases(defaultPath, ""); err != nil {
		return "", "", err
	}
	dir := DatabasesDir(defaultPath)
	if err := ensureDatabaseDirectory(dir); err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, id+".db")
	if databaseIDExists(defaultPath, id, path) {
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
	id, err := canonicalDatabaseID(name)
	if err != nil {
		return "", "", err
	}
	dir := DatabasesDir(defaultPath)
	if err := ensureDatabaseDirectory(dir); err != nil {
		return "", "", err
	}
	targetPath := filepath.Join(dir, id+".db")
	if currentPath == targetPath || (currentPath == defaultPath && id == "default") {
		return "", "", fmt.Errorf("database already has this name")
	}
	if databaseIDExists(defaultPath, id, targetPath) {
		return "", "", fmt.Errorf("database name already exists")
	}
	return id, targetPath, nil
}

func MoveDatabase(currentPath string, targetPath string) error {
	databaseRecoveryMu.Lock()
	defer databaseRecoveryMu.Unlock()
	if _, err := checkedDatabasePath(currentPath); err != nil {
		return err
	}
	if _, err := checkedDatabasePath(targetPath); err != nil {
		return err
	}
	if err := ensureDatabaseDirectory(filepath.Dir(targetPath)); err != nil {
		return err
	}
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
	if _, err := checkedDatabasePath(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := recoverDatabaseDeleteQuarantines(parent); err != nil {
		return err
	}
	pending, err := databaseDeletePendingForPath(parent, path)
	if err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("database delete recovery is still pending")
	}
	ownership, err := db.AcquireDatabaseOwnership(path)
	if err != nil {
		return err
	}
	defer ownership.Close()
	return deleteDatabaseWithOps(path, defaultDatabaseDeleteOps())
}

func databaseDeletePendingForPath(parent, path string) (bool, error) {
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect pending database deletes: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), databaseDeleteQuarantinePrefix) {
			continue
		}
		_, databasePath, _, err := databaseDeleteRecoveryCandidates(parent, filepath.Join(parent, entry.Name()))
		if err != nil {
			return false, err
		}
		if filepath.Clean(databasePath) == filepath.Clean(path) {
			return true, nil
		}
	}
	return false, nil
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
		info, err := ops.lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect database file %q: %w", candidate, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("database file %q is not a regular file", candidate)
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
	if err := writeDatabaseDeleteManifest(quarantineDir, existing, ops); err != nil {
		_ = ops.removeAll(quarantineDir)
		_ = ops.syncDir(parentDir)
		return err
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

func writeDatabaseDeleteManifest(quarantineDir string, paths []string, ops databaseDeleteOps) error {
	manifest := databaseDeleteManifest{Version: 2, Primary: filepath.Base(paths[0]), Files: make([]string, 0, len(paths))}
	for _, path := range paths {
		manifest.Files = append(manifest.Files, filepath.Base(path))
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode database delete manifest: %w", err)
	}
	pending := filepath.Join(quarantineDir, databaseDeleteManifestPendingFile)
	final := filepath.Join(quarantineDir, databaseDeleteManifestFile)
	if err := ops.write(pending, encoded, 0o600); err != nil {
		return fmt.Errorf("write database delete manifest: %w", err)
	}
	if err := ops.syncFile(pending); err != nil {
		return fmt.Errorf("sync database delete manifest: %w", err)
	}
	if err := ops.rename(pending, final); err != nil {
		return fmt.Errorf("publish database delete manifest: %w", err)
	}
	if err := ops.syncDir(quarantineDir); err != nil {
		return fmt.Errorf("sync published database delete manifest: %w", err)
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
	return recoverDatabaseDeleteQuarantinesWithPublish(dir, db.PublishFileNoReplace)
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
		if err := recoverDatabaseDeleteQuarantine(dir, quarantineDir, publish); err != nil {
			return err
		}
	}
	return nil
}

func recoverDatabaseDeleteQuarantine(dir, quarantineDir string, publish func(string, string) error) error {
	markerPath := filepath.Join(quarantineDir, databaseDeleteCompleteMarker)
	marker, markerErr := readDatabaseDeleteMarker(markerPath)
	if markerErr == nil && string(marker) == "complete\n" {
		return removeDatabaseDeleteQuarantine(dir, quarantineDir)
	}
	if markerErr != nil && !os.IsNotExist(markerErr) {
		return fmt.Errorf("read database delete completion marker: %w", markerErr)
	}
	candidates, databasePath, durable, err := databaseDeleteRecoveryCandidates(dir, quarantineDir)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return removeDatabaseDeleteQuarantine(dir, quarantineDir)
	}
	if databasePath == "" {
		return fmt.Errorf("incomplete database delete quarantine has no database file")
	}
	ownership, err := db.AcquireDatabaseOwnership(databasePath)
	if errors.Is(err, db.ErrDatabaseInUse) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim incomplete database delete quarantine: %w", err)
	}
	defer ownership.Close()
	if durable {
		err = recoverManifestedDatabaseDelete(candidates, publish)
	} else {
		err = recoverLegacyDatabaseDelete(candidates, publish)
	}
	if errors.Is(err, errDatabaseDeleteRecoveryConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, item := range candidates {
		if err := syncDatabaseDeletePath(item.original); err != nil {
			return fmt.Errorf("sync recovered database file %q: %w", item.original, err)
		}
	}
	if markerErr == nil {
		if err := os.Remove(markerPath); err != nil {
			return fmt.Errorf("remove invalid database delete completion marker: %w", err)
		}
	}
	return removeDatabaseDeleteQuarantine(dir, quarantineDir)
}

func readDatabaseDeleteMarker(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database delete completion marker is not a regular file")
	}
	return os.ReadFile(path)
}

func databaseDeleteRecoveryCandidates(dir, quarantineDir string) ([]quarantinedDatabaseFile, string, bool, error) {
	manifestPath := filepath.Join(quarantineDir, databaseDeleteManifestFile)
	encoded, err := os.ReadFile(manifestPath)
	if err == nil {
		var manifest databaseDeleteManifest
		if json.Unmarshal(encoded, &manifest) != nil || (manifest.Version != 1 && manifest.Version != 2) || len(manifest.Files) == 0 {
			return nil, "", true, fmt.Errorf("database delete quarantine has an invalid manifest")
		}
		if manifest.Version == 1 {
			for _, name := range manifest.Files {
				if filepath.Ext(name) == ".db" {
					manifest.Primary = name
					break
				}
			}
		}
		if manifest.Primary == "" {
			return nil, "", true, fmt.Errorf("database delete quarantine has no primary artifact")
		}
		if err := validateDatabaseDeleteManifestDirectory(quarantineDir, manifest.Files); err != nil {
			return nil, "", true, err
		}
		return databaseDeleteCandidatesFromNames(dir, quarantineDir, manifest.Files, manifest.Primary, true)
	}
	if !os.IsNotExist(err) {
		return nil, "", true, fmt.Errorf("read database delete manifest: %w", err)
	}
	files, err := os.ReadDir(quarantineDir)
	if err != nil {
		return nil, "", false, fmt.Errorf("inspect incomplete database delete quarantine: %w", err)
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		if file.Name() == databaseDeleteCompleteMarker || file.Name() == databaseDeleteManifestPendingFile {
			continue
		}
		if !databaseCatalogEntryIsRegular(file) {
			return nil, "", false, fmt.Errorf("database delete quarantine contains an unexpected artifact")
		}
		names = append(names, file.Name())
	}
	return databaseDeleteCandidatesFromNames(dir, quarantineDir, names, "", false)
}

func validateDatabaseDeleteManifestDirectory(quarantineDir string, names []string) error {
	allowed := map[string]struct{}{
		databaseDeleteManifestFile: {}, databaseDeleteManifestPendingFile: {}, databaseDeleteCompleteMarker: {},
	}
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	entries, err := os.ReadDir(quarantineDir)
	if err != nil {
		return fmt.Errorf("inspect database delete manifest directory: %w", err)
	}
	for _, entry := range entries {
		if _, ok := allowed[entry.Name()]; !ok || !databaseCatalogEntryIsRegular(entry) {
			return fmt.Errorf("database delete quarantine contains an unexpected artifact")
		}
	}
	return nil
}

func databaseDeleteCandidatesFromNames(dir, quarantineDir string, names []string, primary string, durable bool) ([]quarantinedDatabaseFile, string, bool, error) {
	seen := make(map[string]struct{}, len(names))
	candidates := make([]quarantinedDatabaseFile, 0, len(names))
	databasePath := ""
	for _, name := range names {
		if name == "" || name != filepath.Base(name) || name == databaseDeleteCompleteMarker || name == databaseDeleteManifestFile || name == databaseDeleteManifestPendingFile {
			return nil, "", durable, fmt.Errorf("database delete quarantine contains an invalid artifact name")
		}
		if _, exists := seen[name]; exists {
			return nil, "", durable, fmt.Errorf("database delete quarantine manifest contains a duplicate artifact")
		}
		seen[name] = struct{}{}
		original := filepath.Join(dir, name)
		if (primary != "" && name == primary) || (primary == "" && databasePath == "" && filepath.Ext(name) == ".db") {
			databasePath = original
		}
		candidates = append(candidates, quarantinedDatabaseFile{original: original, quarantine: filepath.Join(quarantineDir, name)})
	}
	return candidates, databasePath, durable, nil
}

func recoverManifestedDatabaseDelete(candidates []quarantinedDatabaseFile, publish func(string, string) error) error {
	for _, item := range candidates {
		quarantined, qErr := regularFileExists(item.quarantine)
		original, oErr := regularFileExists(item.original)
		if qErr != nil || oErr != nil {
			return errors.Join(qErr, oErr)
		}
		switch {
		case quarantined && original:
			return fmt.Errorf("%w %q", errDatabaseDeleteRecoveryConflict, item.original)
		case !quarantined && !original:
			return fmt.Errorf("database delete recovery lost artifact %q", item.original)
		case quarantined:
			if err := publish(item.quarantine, item.original); err != nil {
				return fmt.Errorf("recover database file %q: %w", item.original, err)
			}
		}
	}
	return nil
}

func recoverLegacyDatabaseDelete(candidates []quarantinedDatabaseFile, publish func(string, string) error) error {
	for _, item := range candidates {
		if _, err := os.Lstat(item.original); err == nil || !os.IsNotExist(err) {
			return errDatabaseDeleteRecoveryConflict
		}
	}
	restored := make([]quarantinedDatabaseFile, 0, len(candidates))
	for _, item := range candidates {
		if err := publish(item.quarantine, item.original); err != nil {
			errs := []error{fmt.Errorf("recover database file %q: %w", item.original, err)}
			for index := len(restored) - 1; index >= 0; index-- {
				if rollbackErr := publish(restored[index].original, restored[index].quarantine); rollbackErr != nil {
					errs = append(errs, fmt.Errorf("roll back recovered database file %q: %w", restored[index].original, rollbackErr))
				}
			}
			return errors.Join(errs...)
		}
		restored = append(restored, item)
	}
	return nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database delete artifact %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("database delete artifact %q is not a regular file", path)
	}
	return true, nil
}

func removeDatabaseDeleteQuarantine(parent, quarantine string) error {
	if err := os.RemoveAll(quarantine); err != nil {
		return fmt.Errorf("remove database delete quarantine: %w", err)
	}
	if err := syncDatabaseDeletePath(parent); err != nil {
		return fmt.Errorf("sync removed database delete quarantine: %w", err)
	}
	return nil
}

func DatabasesDir(defaultPath string) string {
	return filepath.Join(filepath.Dir(defaultPath), "databases")
}

func databaseInfo(id string, name string, path string, currentPath string) DatabaseInfo {
	state := "locked"
	if db.LooksLikePlainSQLite(path) {
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

const maxDatabaseIDLength = 63

var (
	databaseIDPattern              = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	legacyDatabaseIDPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{63,251}$`)
	legacyDatabaseReferencePattern = regexp.MustCompile(`^_legacy-[a-f0-9]{55}$`)
)

func validDatabaseID(id string) bool {
	return databaseIDPattern.MatchString(id)
}

func canonicalDatabaseID(name string) (string, error) {
	id := slugifyDatabaseName(name)
	if id == "" {
		return "", fmt.Errorf("database name is required")
	}
	if id == "local-default" {
		return "", fmt.Errorf("database name uses a reserved identifier")
	}
	if len(id) > maxDatabaseIDLength {
		return "", fmt.Errorf("database name must produce an identifier no longer than %d characters", maxDatabaseIDLength)
	}
	if !validDatabaseID(id) {
		return "", fmt.Errorf("database name produces an invalid identifier")
	}
	return id, nil
}

func databaseIDExists(defaultPath, id, path string) bool {
	return databaseCatalogPathOccupied(path) || (id == "default" && databaseCatalogPathOccupied(defaultPath))
}

func databaseCatalogPathOccupied(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !os.IsNotExist(err)
}

func ensureDatabaseDirectory(path string) error {
	if _, err := checkedDatabaseDirectory(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create databases directory: %w", err)
	}
	if _, err := checkedDatabaseDirectory(path); err != nil {
		return err
	}
	return nil
}

func suffixedDatabaseID(base string, sequence int) string {
	suffix := fmt.Sprintf("-%d", sequence)
	maxBaseLength := maxDatabaseIDLength - len(suffix)
	if len(base) > maxBaseLength {
		base = strings.TrimRight(base[:maxBaseLength], "-")
	}
	return base + suffix
}

func validLegacyDatabaseID(id string) bool {
	return legacyDatabaseIDPattern.MatchString(id)
}

func databaseReference(id string) string {
	if validDatabaseID(id) && id != "local-default" {
		return id
	}
	digest := sha256.Sum256([]byte(id))
	return "_legacy-" + hex.EncodeToString(digest[:])[:55]
}

func legacyDatabaseID(defaultPath, reference string) (string, bool, error) {
	if !legacyDatabaseReferencePattern.MatchString(reference) {
		return "", false, nil
	}
	dir := DatabasesDir(defaultPath)
	if _, err := checkedDatabaseDirectory(dir); err != nil {
		return "", false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolve legacy database reference: %w", err)
	}
	for _, entry := range entries {
		if !databaseCatalogEntryIsRegular(entry) || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".db")
		if databaseReference(id) == reference {
			return id, true, nil
		}
	}
	return "", false, nil
}

func databaseCatalogEntryIsRegular(entry os.DirEntry) bool {
	if entry == nil || entry.Type()&os.ModeSymlink != 0 {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular()
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
