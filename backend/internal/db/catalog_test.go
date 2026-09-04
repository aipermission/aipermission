package db

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNewDatabasePathExactRejectsCollisions(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("encrypted default"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewDatabasePathExact(defaultPath, "Default"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected logical default-name collision, got %v", err)
	}
	_, path, err := NewDatabasePathExact(defaultPath, "Recovered Project")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewDatabasePathExact(defaultPath, "Recovered Project"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected exact-name collision, got %v", err)
	}
}

func TestDatabasePathValidationAndDefaultAliases(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	if path, err := DatabasePath(defaultPath, ""); err != nil || path != defaultPath {
		t.Fatalf("empty id should resolve default path, path=%q err=%v", path, err)
	}
	if path, err := DatabasePath(defaultPath, "local-default"); err != nil || path != defaultPath {
		t.Fatalf("local-default should resolve default path, path=%q err=%v", path, err)
	}
	if _, err := DatabasePath(defaultPath, "../bad"); err == nil {
		t.Fatalf("expected invalid database id to fail")
	}
}

func TestDefaultDatabaseNameSwitchesWhenNamedDefaultExists(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	if DefaultDatabaseID(defaultPath) != "default" || DefaultDatabaseName(defaultPath) != "Default" {
		t.Fatalf("unexpected default database metadata")
	}
	defaultNamedPath := filepath.Join(DatabasesDir(defaultPath), "default.db")
	if err := os.MkdirAll(filepath.Dir(defaultNamedPath), 0o700); err != nil {
		t.Fatalf("mkdir databases dir: %v", err)
	}
	if err := os.WriteFile(defaultNamedPath, []byte("db"), 0o600); err != nil {
		t.Fatalf("write named default: %v", err)
	}
	if DefaultDatabaseID(defaultPath) != "local-default" || DefaultDatabaseName(defaultPath) != "Local Default" {
		t.Fatalf("expected local default metadata when databases/default.db exists")
	}
}

func TestNewRenameDeleteAndListDatabases(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")

	id, path, err := NewDatabasePath(defaultPath, "My Project!")
	if err != nil {
		t.Fatalf("new database path: %v", err)
	}
	if id != "my-project" {
		t.Fatalf("unexpected id: %s", id)
	}
	if err := os.WriteFile(path, []byte("encrypted-ish"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}

	nextID, _, err := NewDatabasePath(defaultPath, "My Project!")
	if err != nil {
		t.Fatalf("new duplicate database path: %v", err)
	}
	if nextID != "my-project-2" {
		t.Fatalf("unexpected duplicate id: %s", nextID)
	}

	renamedID, renamedPath, err := RenameDatabase(defaultPath, path, "Renamed Database")
	if err != nil {
		t.Fatalf("rename database: %v", err)
	}
	if renamedID != "renamed-database" {
		t.Fatalf("unexpected renamed id: %s", renamedID)
	}
	if Exists(path) || !Exists(renamedPath) {
		t.Fatalf("rename did not move database")
	}

	items, err := ListDatabases(defaultPath, renamedPath)
	if err != nil {
		t.Fatalf("list databases: %v", err)
	}
	if len(items) != 1 || !items[0].Current || items[0].Name != "renamed database" {
		t.Fatalf("unexpected database list: %#v", items)
	}

	if err := DeleteDatabase(renamedPath); err != nil {
		t.Fatalf("delete database: %v", err)
	}
	if Exists(renamedPath) {
		t.Fatalf("database should be deleted")
	}
}

func TestDeleteDatabaseRollsBackEveryQuarantineRenameFailure(t *testing.T) {
	for _, failedCandidate := range []int{0, 1, 2} {
		t.Run(strconv.Itoa(failedCandidate), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollback.db")
			candidates := []string{path, path + "-wal", path + "-shm"}
			for index, candidate := range candidates {
				if err := os.WriteFile(candidate, []byte{byte(index + 1)}, 0o600); err != nil {
					t.Fatalf("seed candidate: %v", err)
				}
			}
			ops := defaultDatabaseDeleteOps()
			ops.rename = func(oldPath string, newPath string) error {
				if oldPath == candidates[failedCandidate] {
					return errors.New("injected rename failure")
				}
				return os.Rename(oldPath, newPath)
			}
			if err := deleteDatabaseWithOps(path, ops); err == nil || !strings.Contains(err.Error(), "injected rename failure") {
				t.Fatalf("expected injected rename failure, got %v", err)
			}
			for _, candidate := range candidates {
				if _, err := os.Stat(candidate); err != nil {
					t.Fatalf("candidate %q was not rolled back: %v", candidate, err)
				}
			}
			quarantined, err := filepath.Glob(filepath.Join(filepath.Dir(path), databaseDeleteQuarantinePrefix+"*"))
			if err != nil || len(quarantined) != 0 {
				t.Fatalf("rollback left quarantined files: paths=%v err=%v", quarantined, err)
			}
		})
	}
}

func TestDeleteDatabaseDefersFailedQuarantineCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.db")
	if err := os.WriteFile(path, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := defaultDatabaseDeleteOps()
	ops.removeAll = func(string) error {
		return errors.New("injected cleanup failure")
	}
	if err := deleteDatabaseWithOps(path, ops); err != nil {
		t.Fatalf("completed quarantine should be a successful logical delete: %v", err)
	}
	if Exists(path) {
		t.Fatalf("database should no longer be addressable after quarantine")
	}
	quarantined, err := filepath.Glob(filepath.Join(filepath.Dir(path), databaseDeleteQuarantinePrefix+"*"))
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("expected deferred quarantine cleanup: paths=%v err=%v", quarantined, err)
	}
	if err := recoverDatabaseDeleteQuarantines(filepath.Dir(path)); err != nil {
		t.Fatalf("retry deferred cleanup: %v", err)
	}
	quarantined, err = filepath.Glob(filepath.Join(filepath.Dir(path), databaseDeleteQuarantinePrefix+"*"))
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("expected deferred cleanup to finish: paths=%v err=%v", quarantined, err)
	}
}

func TestDeleteDatabaseRemovesMigrationRecoveryArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.db")
	candidates := []string{
		path,
		path + ".pre-migration-v18.aipdb",
		path + ".pre-migration-v19.aipdb",
		path + ".pre-migration-v20.aipdb.pending",
	}
	for _, candidate := range candidates {
		if err := os.WriteFile(candidate, []byte("encrypted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteDatabase(path); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if Exists(candidate) {
			t.Fatalf("database delete retained recovery artifact %q", candidate)
		}
	}
}

func TestDeleteDatabaseRollsBackWhenCompletionMarkerFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker.db")
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(candidate, []byte("preserved"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ops := defaultDatabaseDeleteOps()
	ops.write = func(string, []byte, os.FileMode) error {
		return errors.New("injected marker failure")
	}
	if err := deleteDatabaseWithOps(path, ops); err == nil || !strings.Contains(err.Error(), "injected marker failure") {
		t.Fatalf("expected marker failure, got %v", err)
	}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if !Exists(candidate) {
			t.Fatalf("marker failure did not restore %q", candidate)
		}
	}
}

func TestDeleteDatabaseRollsBackWhenQuarantineDurabilityFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.db")
	if err := os.WriteFile(path, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := defaultDatabaseDeleteOps()
	ops.syncFile = func(syncPath string) error {
		if strings.Contains(syncPath, databaseDeleteQuarantinePrefix) && filepath.Base(syncPath) != databaseDeleteCompleteMarker {
			return errors.New("injected file sync failure")
		}
		return syncDatabaseDeletePath(syncPath)
	}
	if err := deleteDatabaseWithOps(path, ops); err == nil || !strings.Contains(err.Error(), "injected file sync failure") {
		t.Fatalf("expected sync failure, got %v", err)
	}
	if !Exists(path) {
		t.Fatal("sync failure did not restore the database")
	}
	quarantines, err := filepath.Glob(filepath.Join(filepath.Dir(path), databaseDeleteQuarantinePrefix+"*"))
	if err != nil || len(quarantines) != 0 {
		t.Fatalf("sync rollback left quarantine state: paths=%v err=%v", quarantines, err)
	}
}

func TestDeleteDatabaseSyncsMovedFilesAndCompletionMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durable.db")
	if err := os.WriteFile(path, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := defaultDatabaseDeleteOps()
	syncedFiles := []string{}
	syncedDirs := []string{}
	ops.syncFile = func(path string) error {
		syncedFiles = append(syncedFiles, filepath.Base(path))
		return nil
	}
	ops.syncDir = func(path string) error {
		syncedDirs = append(syncedDirs, path)
		return nil
	}
	if err := deleteDatabaseWithOps(path, ops); err != nil {
		t.Fatal(err)
	}
	if !containsString(syncedFiles, filepath.Base(path)) || !containsString(syncedFiles, databaseDeleteCompleteMarker) {
		t.Fatalf("durable delete did not sync data and marker files: %#v", syncedFiles)
	}
	if len(syncedDirs) < 3 {
		t.Fatalf("durable delete did not sync quarantine and parent directories: %#v", syncedDirs)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDeleteDatabasePreservesQuarantineWhenRollbackRenameFails(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "rollback-recovery.db")
	walPath := path + "-wal"
	for _, candidate := range []string{path, walPath} {
		if err := os.WriteFile(candidate, []byte(filepath.Base(candidate)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ops := defaultDatabaseDeleteOps()
	restoreFailed := false
	ops.rename = func(oldPath string, newPath string) error {
		if oldPath == walPath {
			return errors.New("injected quarantine failure")
		}
		if strings.Contains(oldPath, databaseDeleteQuarantinePrefix) && newPath == path && !restoreFailed {
			restoreFailed = true
			return errors.New("injected rollback failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if err := deleteDatabaseWithOps(path, ops); err == nil || !strings.Contains(err.Error(), "injected rollback failure") {
		t.Fatalf("expected rollback failure, got %v", err)
	}
	if Exists(path) {
		t.Fatal("failed rollback should leave the database in quarantine")
	}
	quarantines, err := filepath.Glob(filepath.Join(directory, databaseDeleteQuarantinePrefix+"*"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("failed rollback must retain one recovery quarantine: paths=%v err=%v", quarantines, err)
	}
	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover retained rollback quarantine: %v", err)
	}
	for _, candidate := range []string{path, walPath} {
		if !Exists(candidate) {
			t.Fatalf("recovery did not restore %q", candidate)
		}
	}
}

func TestDeleteDatabaseRemovesMarkerBeforeFailedDurabilityRollback(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "marker-rollback.db")
	if err := os.WriteFile(path, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := defaultDatabaseDeleteOps()
	ops.syncFile = func(syncPath string) error {
		if filepath.Base(syncPath) == databaseDeleteCompleteMarker {
			return errors.New("injected marker sync failure")
		}
		return syncDatabaseDeletePath(syncPath)
	}
	restoreFailed := false
	ops.rename = func(oldPath string, newPath string) error {
		if strings.Contains(oldPath, databaseDeleteQuarantinePrefix) && newPath == path && !restoreFailed {
			restoreFailed = true
			return errors.New("injected rollback failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if err := deleteDatabaseWithOps(path, ops); err == nil || !strings.Contains(err.Error(), "injected marker sync failure") || !strings.Contains(err.Error(), "injected rollback failure") {
		t.Fatalf("expected marker sync and rollback failures, got %v", err)
	}
	quarantines, err := filepath.Glob(filepath.Join(directory, databaseDeleteQuarantinePrefix+"*"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("expected retained recovery quarantine: paths=%v err=%v", quarantines, err)
	}
	if Exists(filepath.Join(quarantines[0], databaseDeleteCompleteMarker)) {
		t.Fatal("failed rollback retained a completion marker that could destroy recoverable data")
	}
	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover durability rollback quarantine: %v", err)
	}
	if !Exists(path) {
		t.Fatal("recovery did not restore the database")
	}
}

func TestDatabaseCatalogRecoversInterruptedDeleteQuarantine(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	quarantineDir := filepath.Join(filepath.Dir(defaultPath), databaseDeleteQuarantinePrefix+"interrupted")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{defaultPath, defaultPath + "-wal"} {
		if err := os.WriteFile(filepath.Join(quarantineDir, filepath.Base(candidate)), []byte("preserved"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		t.Fatalf("list databases after interrupted delete: %v", err)
	}
	if len(items) != 1 || !Exists(defaultPath) || !Exists(defaultPath+"-wal") {
		t.Fatalf("interrupted delete was not rolled back: items=%#v", items)
	}
	if Exists(quarantineDir) {
		t.Fatalf("recovered quarantine directory should be removed")
	}
}
