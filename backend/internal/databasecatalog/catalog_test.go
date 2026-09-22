package databasecatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
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

func TestCatalogRejectsSymlinkedDatabaseEntries(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	outside := filepath.Join(root, "outside.db")
	if err := os.WriteFile(outside, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := DatabasesDir(defaultPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyID := strings.Repeat("a", 64)
	link := filepath.Join(directory, legacyID+".db")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("symlinked database was listed: %#v", items)
	}
	if _, err := DatabasePath(defaultPath, databaseReference(legacyID)); err == nil {
		t.Fatal("symlinked legacy database reference was resolved")
	}
	if _, err := DatabasePath(defaultPath, legacyID); err == nil {
		t.Fatal("symlinked legacy database id was resolved")
	}
}

func TestCatalogMutationsRejectSymlinkedDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, DatabasesDir(defaultPath)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := NewDatabasePath(defaultPath, "Project"); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("NewDatabasePath followed a symlinked database directory: %v", err)
	}
	if _, _, err := NewDatabasePathExact(defaultPath, "Project"); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("NewDatabasePathExact followed a symlinked database directory: %v", err)
	}
	if _, _, err := RenameDatabaseTarget(defaultPath, defaultPath, "Project"); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("RenameDatabaseTarget followed a symlinked database directory: %v", err)
	}
	targetPath := filepath.Join(DatabasesDir(defaultPath), "project.db")
	if err := MoveDatabase(defaultPath, targetPath); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("MoveDatabase followed a symlinked database directory: %v", err)
	}
	if err := DeleteDatabase(targetPath); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("DeleteDatabase followed a symlinked database directory: %v", err)
	}
}

func TestNewDatabasePathTreatsUnexpectedArtifactsAsOccupied(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.db")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "project.db")); err != nil {
		t.Fatal(err)
	}

	id, path, err := NewDatabasePath(defaultPath, "Project")
	if err != nil {
		t.Fatal(err)
	}
	if id != "project-2" || filepath.Base(path) != "project-2.db" {
		t.Fatalf("unexpected artifact must reserve its identifier: id=%q path=%q", id, path)
	}
	if _, _, err := NewDatabasePathExact(defaultPath, "Project"); !errors.Is(err, ErrDatabaseExists) {
		t.Fatalf("exact allocation should reject an occupied symlink path: %v", err)
	}
}

func TestDatabaseIDsRoundTripAtBoundaryAndKeepCollisionSuffixBounded(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	name := strings.Repeat("a", maxDatabaseIDLength)
	id, path, err := NewDatabasePath(defaultPath, name)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := DatabasePath(defaultPath, id); err != nil || resolved != path {
		t.Fatalf("created database must resolve to its path: id=%q path=%q resolved=%q err=%v", id, path, resolved, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	nextID, nextPath, err := NewDatabasePath(defaultPath, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextID) != maxDatabaseIDLength || !strings.HasSuffix(nextID, "-2") {
		t.Fatalf("collision suffix must stay within the identifier limit: %q", nextID)
	}
	if resolved, err := DatabasePath(defaultPath, nextID); err != nil || resolved != nextPath {
		t.Fatalf("suffixed database must resolve to its path: id=%q path=%q resolved=%q err=%v", nextID, nextPath, resolved, err)
	}
}

func TestDatabaseNameValidationRejectsReservedAndOverlongIDs(t *testing.T) {
	defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
	currentPath := filepath.Join(DatabasesDir(defaultPath), "current.db")
	for _, name := range []string{"Local Default", strings.Repeat("a", maxDatabaseIDLength+1), "世界"} {
		if _, _, err := NewDatabasePath(defaultPath, name); err == nil {
			t.Fatalf("NewDatabasePath accepted invalid name %q", name)
		}
		if _, _, err := NewDatabasePathExact(defaultPath, name); err == nil {
			t.Fatalf("NewDatabasePathExact accepted invalid name %q", name)
		}
		if _, _, err := RenameDatabaseTarget(defaultPath, currentPath, name); err == nil {
			t.Fatalf("RenameDatabaseTarget accepted invalid name %q", name)
		}
	}
}

func TestDatabaseCatalogRecoversLegacyReservedAndOverlongIDsWithoutMovingData(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("root default"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := DatabasesDir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyIDs := []string{"local-default", strings.Repeat("b", maxDatabaseIDLength+1)}
	for _, id := range legacyIDs {
		if err := os.WriteFile(filepath.Join(dir, id+".db"), []byte("legacy "+id), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		t.Fatal(err)
	}
	legacyPaths := map[string]bool{}
	for _, item := range items {
		if !strings.HasPrefix(item.ID, "_legacy-") {
			continue
		}
		resolved, err := DatabasePath(defaultPath, item.ID)
		if err != nil {
			t.Fatalf("resolve recovery reference %q: %v", item.ID, err)
		}
		legacyPaths[resolved] = true
	}
	for _, id := range legacyIDs {
		path := filepath.Join(dir, id+".db")
		if !legacyPaths[path] || !db.Exists(path) {
			t.Fatalf("legacy database was not recoverable in place: id=%q items=%#v", id, items)
		}
	}
	if resolved, err := DatabasePath(defaultPath, "local-default"); err != nil || resolved != defaultPath {
		t.Fatalf("reserved root alias changed: resolved=%q err=%v", resolved, err)
	}
	if resolved, err := DatabasePath(defaultPath, legacyIDs[1]); err != nil || resolved != filepath.Join(dir, legacyIDs[1]+".db") {
		t.Fatalf("legacy raw overlong id should remain recoverable: resolved=%q err=%v", resolved, err)
	}
}

func FuzzDatabaseIDRoundTrip(f *testing.F) {
	for _, name := range []string{"Project One", "default", strings.Repeat("z", maxDatabaseIDLength), "Local Default", "世界"} {
		f.Add(name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		defaultPath := filepath.Join(t.TempDir(), "aipermission.db")
		id, path, err := NewDatabasePath(defaultPath, name)
		if err != nil {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
			t.Fatal(err)
		}
		resolved, err := DatabasePath(defaultPath, id)
		if err != nil || resolved != path {
			t.Fatalf("accepted name did not round-trip: name=%q id=%q path=%q resolved=%q err=%v", name, id, path, resolved, err)
		}
	})
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
	if DefaultDatabaseID(defaultPath) != "default" || DefaultDatabaseName(defaultPath) != "Default" {
		t.Fatalf("named Default must remain directly addressable when the root database is absent")
	}
	if resolved, err := DatabasePath(defaultPath, DefaultDatabaseID(defaultPath)); err != nil || resolved != defaultNamedPath {
		t.Fatalf("named Default did not reopen after restart: path=%q err=%v", resolved, err)
	}
	if err := os.WriteFile(defaultPath, []byte("root db"), 0o600); err != nil {
		t.Fatalf("write root default: %v", err)
	}
	if DefaultDatabaseID(defaultPath) != "local-default" || DefaultDatabaseName(defaultPath) != "Local Default" {
		t.Fatalf("expected local default metadata when both default databases exist")
	}
}

func TestZeroByteNamedDefaultRoundTripsWithoutAliasingRootDatabase(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("root database"), 0o600); err != nil {
		t.Fatal(err)
	}
	namedPath := filepath.Join(DatabasesDir(defaultPath), "default.db")
	if err := os.MkdirAll(filepath.Dir(namedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(namedPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := ListDatabases(defaultPath, "")
	if err != nil || len(items) != 2 || items[0].ID != "local-default" || items[1].ID != "default" {
		t.Fatalf("catalog items=%#v err=%v", items, err)
	}
	resolved, err := DatabasePath(defaultPath, "default")
	if err != nil || resolved != namedPath {
		t.Fatalf("named default resolved=%q err=%v", resolved, err)
	}
	if err := DeleteDatabase(resolved); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(defaultPath); err != nil || string(content) != "root database" {
		t.Fatalf("root database changed: content=%q err=%v", content, err)
	}
}

func TestDatabaseCatalogRejectsSymlinkedDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(filepath.Join(outside, "escaped.db"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, DatabasesDir(defaultPath)); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDatabases(defaultPath, ""); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("symlinked database directory list error=%v", err)
	}
	if _, err := DatabasePath(defaultPath, "escaped"); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("symlinked database directory resolve error=%v", err)
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
	if db.Exists(path) || !db.Exists(renamedPath) {
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
	if db.Exists(renamedPath) {
		t.Fatalf("database should be deleted")
	}
}

func TestMoveDatabasePreservesSidecarsAndRecoveryArtifactsDurably(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "current.db")
	targetPath := filepath.Join(root, "databases", "renamed.db")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	suffixes := []string{"", "-wal", "-shm", ".pre-migration-v24.aipdb", ".pre-migration-v25.aipdb.pending"}
	for _, suffix := range suffixes {
		if err := os.WriteFile(currentPath+suffix, []byte("preserved"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := MoveDatabase(currentPath, targetPath); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range suffixes {
		if db.Exists(currentPath+suffix) || !db.Exists(targetPath+suffix) {
			t.Fatalf("artifact suffix %q was not moved", suffix)
		}
	}
}

func TestMoveDatabaseRollsBackPartialArtifactMove(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "current.db")
	targetPath := filepath.Join(root, "renamed.db")
	for _, path := range []string{currentPath, currentPath + "-wal"} {
		if err := os.WriteFile(path, []byte("preserved"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ops := defaultDatabaseMoveOps()
	moveCount := 0
	ops.rename = func(source, target string) error {
		moveCount++
		if moveCount == 2 {
			return errors.New("injected move failure")
		}
		return os.Rename(source, target)
	}
	if err := moveDatabaseWithOps(currentPath, targetPath, ops); err == nil || !strings.Contains(err.Error(), "injected move failure") {
		t.Fatalf("expected move failure, got %v", err)
	}
	for _, path := range []string{currentPath, currentPath + "-wal"} {
		if !db.Exists(path) {
			t.Fatalf("rollback did not restore %q", path)
		}
	}
	if db.Exists(targetPath) {
		t.Fatal("rollback retained target database")
	}
}

func TestMoveDatabaseRetriesIndeterminatePlatformRollbackBeforeRemovingJournal(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "current.db")
	targetPath := filepath.Join(root, "renamed.db")
	if err := os.WriteFile(currentPath, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}

	moveErr := errors.New("injected platform rollback failure")
	ops := defaultDatabaseMoveOps()
	ops.rename = func(source, target string) error {
		if source == currentPath && target == targetPath {
			if err := os.Rename(source, target); err != nil {
				return err
			}
			return errors.Join(errDurableFileMoveStateIndeterminate, moveErr)
		}
		return os.Rename(source, target)
	}

	err := moveDatabaseWithOps(currentPath, targetPath, ops)
	if !errors.Is(err, moveErr) {
		t.Fatalf("move error = %v, want platform rollback failure", err)
	}
	if content, readErr := os.ReadFile(currentPath); readErr != nil || string(content) != "preserved" {
		t.Fatalf("restored source = %q, err=%v", content, readErr)
	}
	if db.Exists(targetPath) {
		t.Fatal("outer rollback retained indeterminate target")
	}
	journals, globErr := filepath.Glob(filepath.Join(root, databaseMoveJournalPrefix+"*"))
	if globErr != nil || len(journals) != 0 {
		t.Fatalf("completed rollback retained journals %v, err=%v", journals, globErr)
	}
}

func TestMoveDatabasePreservesCompletedTargetWhenMarkerCannotBeRemoved(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "current.db")
	targetPath := filepath.Join(root, "renamed.db")
	if err := os.WriteFile(currentPath, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := defaultDatabaseMoveOps()
	rootSyncs := 0
	ops.syncDir = func(path string) error {
		if path == root {
			rootSyncs++
			if rootSyncs == 3 {
				return errors.New("injected final root sync failure")
			}
		}
		return syncDatabaseDeletePath(path)
	}
	ops.remove = func(path string) error {
		if filepath.Base(path) == databaseMoveCompleteFile {
			return errors.New("injected marker removal failure")
		}
		return os.Remove(path)
	}
	if err := moveDatabaseWithOps(currentPath, targetPath, ops); err == nil ||
		!strings.Contains(err.Error(), "injected marker removal failure") {
		t.Fatalf("expected uncertain completion failure, got %v", err)
	}
	if db.Exists(currentPath) || !db.Exists(targetPath) {
		t.Fatal("durably marked target must remain authoritative")
	}
	if err := recoverDatabaseMoveJournals(root); err != nil {
		t.Fatalf("recover durably completed move: %v", err)
	}
	if db.Exists(currentPath) || !db.Exists(targetPath) {
		t.Fatal("startup recovery did not preserve completed target")
	}
}

func TestDatabaseCatalogRecoversInterruptedMoveJournal(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	targetPath := filepath.Join(root, "databases", "renamed.db")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"interrupted")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := databaseMoveManifest{
		SourceBase: defaultPath,
		TargetBase: targetPath,
		Moves: []databaseMove{
			{Source: defaultPath, Target: targetPath},
			{Source: defaultPath + "-wal", Target: targetPath + "-wal"},
		},
	}
	writeDatabaseMoveManifestFixture(t, journalDir, manifest)
	if err := os.Rename(defaultPath, targetPath); err != nil {
		t.Fatal(err)
	}

	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		t.Fatalf("list databases after interrupted move: %v", err)
	}
	if len(items) != 1 || !db.Exists(defaultPath) || !db.Exists(defaultPath+"-wal") {
		t.Fatalf("interrupted move was not rolled back: items=%#v", items)
	}
	if db.Exists(targetPath) || db.Exists(journalDir) {
		t.Fatal("interrupted move recovery retained target or journal")
	}
}

func TestMoveRecoveryDoesNotPartiallyRestoreConflictingArtifactSet(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "target.db")
	for _, path := range []string{source, target, target + "-wal"} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := databaseMoveManifest{
		SourceBase: source, TargetBase: target,
		Moves: []databaseMove{{Source: source, Target: target}, {Source: source + "-wal", Target: target + "-wal"}},
	}
	if err := recoverDatabaseMoveJournal(manifest); err == nil || !strings.Contains(err.Error(), "duplicate artifact state") {
		t.Fatalf("recovery error = %v", err)
	}
	if db.Exists(source+"-wal") || !db.Exists(target+"-wal") {
		t.Fatal("conflicting move recovery partially restored the WAL")
	}
}

func TestMoveRecoveryRemovesInterruptedHardLinkPublication(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(source, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, target); err != nil {
		t.Fatal(err)
	}
	manifest := databaseMoveManifest{
		SourceBase: source, TargetBase: target,
		Moves: []databaseMove{{Source: source, Target: target}},
	}
	if err := recoverDatabaseMoveJournal(manifest); err != nil {
		t.Fatalf("recover interrupted hard-link publication: %v", err)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "database" {
		t.Fatalf("recovered source content = %q, err=%v", content, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("duplicate target remains after recovery: %v", err)
	}
}

func TestMoveRecoveryRestoresZeroByteArtifacts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := databaseMoveManifest{
		SourceBase: source, TargetBase: target,
		Moves: []databaseMove{{Source: source, Target: target}},
	}
	if err := recoverDatabaseMoveJournal(manifest); err != nil {
		t.Fatalf("recover zero-byte move artifact: %v", err)
	}
	if info, err := os.Lstat(source); err != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("zero-byte source was not restored: info=%v err=%v", info, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("zero-byte target remains after recovery: %v", err)
	}
}

func TestMoveManifestRejectsSymlinkedCatalogDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "databases")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "databases", "target.db")
	manifest := databaseMoveManifest{
		SourceBase: source, TargetBase: target,
		Moves: []databaseMove{{Source: source, Target: target}},
	}
	if err := validateDatabaseMoveManifest(root, manifest); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("symlinked catalog directory validation = %v", err)
	}
}

func TestMoveManifestRejectsNonCanonicalArtifactPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "target.db")
	manifest := databaseMoveManifest{
		SourceBase: source,
		TargetBase: target,
		Moves: []databaseMove{{
			Source: filepath.Join(root, "nested") + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(source),
			Target: target,
		}},
	}
	if err := validateDatabaseMoveManifest(root, manifest); err == nil || !strings.Contains(err.Error(), "invalid artifact path") {
		t.Fatalf("non-canonical move artifact validation = %v", err)
	}
}

func TestCompletedMoveCleanupKeepsMarkerUntilArtifactsAreDurablyRemoved(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"cleanup")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(journalDir, databaseMoveManifestFile)
	markerPath := filepath.Join(journalDir, databaseMoveCompleteFile)
	for path, content := range map[string]string{manifestPath: `{}`, markerPath: "complete\n"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ops := defaultDatabaseMoveOps()
	ops.remove = func(path string) error {
		if path == manifestPath {
			return errors.New("injected artifact cleanup failure")
		}
		return os.Remove(path)
	}
	if err := removeCompletedMoveJournalWithOps(root, journalDir, ops); err == nil || !strings.Contains(err.Error(), "injected artifact cleanup failure") {
		t.Fatalf("completed move cleanup error = %v", err)
	}
	if marker, err := os.ReadFile(markerPath); err != nil || string(marker) != "complete\n" {
		t.Fatalf("completion marker was removed before journal artifacts: marker=%q err=%v", marker, err)
	}
}

func TestMoveRecoveryRollsBackPartialPublishFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	target := filepath.Join(root, "target.db")
	for _, path := range []string{target, target + "-wal"} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := databaseMoveManifest{
		SourceBase: source, TargetBase: target,
		Moves: []databaseMove{{Source: source, Target: target}, {Source: source + "-wal", Target: target + "-wal"}},
	}
	publishCount := 0
	publish := func(currentPath, nextPath string) error {
		publishCount++
		if publishCount == 2 {
			return errors.New("injected recovery publish failure")
		}
		return os.Rename(currentPath, nextPath)
	}
	if err := recoverDatabaseMoveJournalWithPublish(manifest, publish); err == nil || !strings.Contains(err.Error(), "injected recovery publish failure") {
		t.Fatalf("recovery error = %v", err)
	}
	if db.Exists(source) || db.Exists(source+"-wal") || !db.Exists(target) || !db.Exists(target+"-wal") {
		t.Fatal("failed move recovery left a partially restored artifact set")
	}
}

func TestDatabaseCatalogRecoversInterruptedNamedDatabaseMoveJournal(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	databaseDir := DatabasesDir(defaultPath)
	sourcePath := filepath.Join(databaseDir, "source.db")
	targetPath := filepath.Join(databaseDir, "renamed.db")
	if err := os.MkdirAll(databaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(databaseDir, databaseMoveJournalPrefix+"interrupted-named")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDatabaseMoveManifestFixture(t, journalDir, databaseMoveManifest{
		SourceBase: sourcePath,
		TargetBase: targetPath,
		Moves:      []databaseMove{{Source: sourcePath, Target: targetPath}},
	})
	if err := os.Rename(sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}

	items, err := ListDatabases(defaultPath, "")
	if err != nil {
		t.Fatalf("list databases after interrupted named move: %v", err)
	}
	if len(items) != 1 || items[0].ID != "source" || !db.Exists(sourcePath) {
		t.Fatalf("interrupted named move was not rolled back: items=%#v", items)
	}
	if db.Exists(targetPath) || db.Exists(journalDir) {
		t.Fatal("interrupted named move recovery retained target or journal")
	}
}

func TestDatabaseCatalogFinishesCompletedMoveJournalCleanup(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	targetPath := filepath.Join(root, "databases", "renamed.db")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"completed")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDatabaseMoveManifestFixture(t, journalDir, databaseMoveManifest{
		SourceBase: defaultPath,
		TargetBase: targetPath,
		Moves:      []databaseMove{{Source: defaultPath, Target: targetPath}},
	})
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveCompleteFile), []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := ListDatabases(defaultPath, targetPath)
	if err != nil {
		t.Fatalf("list databases after completed move: %v", err)
	}
	if len(items) != 1 || !items[0].Current || !db.Exists(targetPath) || db.Exists(journalDir) {
		t.Fatalf("completed move cleanup mismatch: items=%#v", items)
	}
}

func TestDatabaseCatalogDiscardsUnpublishedMoveJournal(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"truncated")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveManifestFile+".pending"), []byte(`{"source_base":`), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := ListDatabases(defaultPath, defaultPath)
	if err != nil {
		t.Fatalf("truncated unpublished journal blocked catalog: %v", err)
	}
	if len(items) != 1 || !db.Exists(defaultPath) || db.Exists(journalDir) {
		t.Fatalf("unpublished journal recovery mismatch: items=%#v", items)
	}
}

func TestDatabaseCatalogRejectsCorruptPublishedMoveJournal(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"corrupt-published")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveManifestFile), []byte(`{"source_base":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ListDatabases(defaultPath, defaultPath); err == nil || !strings.Contains(err.Error(), "decode database move journal") {
		t.Fatalf("expected corrupt published journal to fail closed, got %v", err)
	}
	if _, err := os.Stat(journalDir); !db.Exists(defaultPath) || err != nil {
		t.Fatal("failed recovery must preserve the database and journal for manual inspection")
	}
}

func TestDatabaseCatalogCompletedMoveJournalDoesNotOwnLaterTargetState(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	targetPath := filepath.Join(root, "databases", "renamed.db")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"completed-cleanup")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDatabaseMoveManifestFixture(t, journalDir, databaseMoveManifest{
		SourceBase: defaultPath,
		TargetBase: targetPath,
		Moves:      []databaseMove{{Source: defaultPath, Target: targetPath}},
	})
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveCompleteFile), []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDatabases(defaultPath, ""); err != nil {
		t.Fatalf("stale completed journal blocked catalog: %v", err)
	}
	if db.Exists(journalDir) {
		t.Fatal("stale completed journal was not removed")
	}
}

func TestDatabaseCatalogCompletedMoveJournalDoesNotRequireManifest(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"completed-corrupt-manifest")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveManifestFile), []byte(`{"source_base":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveCompleteFile), []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := ListDatabases(defaultPath, defaultPath)
	if err != nil {
		t.Fatalf("completed move journal with corrupt manifest blocked catalog: %v", err)
	}
	if len(items) != 1 || !db.Exists(defaultPath) || db.Exists(journalDir) {
		t.Fatalf("completed move journal cleanup mismatch: items=%#v", items)
	}
}

func TestDatabaseCatalogRejectsInvalidMoveCompletionMarker(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Join(root, databaseMoveJournalPrefix+"invalid-completion")
	if err := os.Mkdir(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveCompleteFile), []byte("not-complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ListDatabases(defaultPath, defaultPath); err == nil || !strings.Contains(err.Error(), "invalid completion marker") {
		t.Fatalf("expected invalid completion marker to fail closed, got %v", err)
	}
	if _, err := os.Stat(journalDir); !db.Exists(defaultPath) || err != nil {
		t.Fatal("failed recovery must preserve the database and journal for manual inspection")
	}
}

func TestDatabaseCatalogRejectsSymlinkedMoveJournalFiles(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		journalFile string
		content     string
	}{
		{name: "completion marker", journalFile: databaseMoveCompleteFile, content: "complete\n"},
		{name: "manifest", journalFile: databaseMoveManifestFile, content: `{}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			defaultPath := filepath.Join(root, "aipermission.db")
			if err := os.WriteFile(defaultPath, []byte("database"), 0o600); err != nil {
				t.Fatal(err)
			}
			journalDir := filepath.Join(root, databaseMoveJournalPrefix+"symlink")
			if err := os.Mkdir(journalDir, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(root, "outside")
			if err := os.WriteFile(outside, []byte(testCase.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(journalDir, testCase.journalFile)); err != nil {
				t.Fatal(err)
			}

			if _, err := ListDatabases(defaultPath, defaultPath); err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Fatalf("symlinked %s did not fail closed: %v", testCase.name, err)
			}
			if !databaseCatalogFileExists(defaultPath) {
				t.Fatal("failed recovery removed the database")
			}
		})
	}
}

func writeDatabaseMoveManifestFixture(t *testing.T, journalDir string, manifest databaseMoveManifest) {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, databaseMoveManifestFile), encoded, 0o600); err != nil {
		t.Fatal(err)
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
	remove := ops.remove
	failed := false
	ops.remove = func(path string) error {
		if !failed && filepath.Base(path) == databaseDeleteManifestFile {
			failed = true
			return errors.New("injected cleanup failure")
		}
		return remove(path)
	}
	if err := deleteDatabaseWithOps(path, ops); err != nil {
		t.Fatalf("completed quarantine should be a successful logical delete: %v", err)
	}
	if db.Exists(path) {
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

func TestCompletedDeleteCleanupKeepsMarkerUntilArtifactsAreDurablyRemoved(t *testing.T) {
	root := t.TempDir()
	quarantineDir := filepath.Join(root, databaseDeleteQuarantinePrefix+"cleanup-order")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(quarantineDir, databaseDeleteManifestFile)
	markerPath := filepath.Join(quarantineDir, databaseDeleteCompleteMarker)
	for path, content := range map[string]string{manifestPath: `{}`, markerPath: "complete\n"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ops := defaultDatabaseDeleteOps()
	ops.remove = func(path string) error {
		if path == manifestPath {
			return errors.New("injected artifact cleanup failure")
		}
		return os.Remove(path)
	}
	if err := removeCompletedDatabaseDeleteQuarantine(root, quarantineDir, ops); err == nil || !strings.Contains(err.Error(), "injected artifact cleanup failure") {
		t.Fatalf("completed delete cleanup error = %v", err)
	}
	if marker, err := os.ReadFile(markerPath); err != nil || string(marker) != "complete\n" {
		t.Fatalf("completion marker was removed before quarantine artifacts: marker=%q err=%v", marker, err)
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
		if db.Exists(candidate) {
			t.Fatalf("database delete retained recovery artifact %q", candidate)
		}
	}
}

func TestDeleteDatabaseRejectsSymlinkedArtifacts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog.db")
	target := filepath.Join(root, "outside.db")
	if err := os.WriteFile(target, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	err := deleteDatabaseWithOps(path, defaultDatabaseDeleteOps())
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected symlinked database rejection, got %v", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "preserved" {
		t.Fatalf("symlink target changed: content=%q err=%v", content, readErr)
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
		if !db.Exists(candidate) {
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
	if !db.Exists(path) {
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
	if db.Exists(path) {
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
		if !db.Exists(candidate) {
			t.Fatalf("recovery did not restore %q", candidate)
		}
	}
}

func TestDeleteDatabaseRejectsUnresolvedQuarantineForSamePath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "conflicted.db")
	if err := os.WriteFile(path, []byte("later database"), 0o600); err != nil {
		t.Fatal(err)
	}
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"conflict")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if err := os.WriteFile(filepath.Join(quarantineDir, name), []byte("original database"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(databaseDeleteManifest{Version: 2, Primary: name, Files: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := DeleteDatabase(path); err == nil || !strings.Contains(err.Error(), "recovery is still pending") {
		t.Fatalf("conflicted quarantine delete = %v", err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "later database" {
		t.Fatalf("delete changed conflicting destination: content=%q err=%v", content, err)
	}
}

func TestDeleteRecoveryRollsBackPartialPublishFailure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "partial-recovery.db")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"partial")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + "-wal"} {
		if err := os.WriteFile(filepath.Join(quarantineDir, filepath.Base(candidate)), []byte("preserved"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	publishCount := 0
	publish := func(currentPath, nextPath string) error {
		publishCount++
		if publishCount == 2 {
			return errors.New("injected delete recovery publish failure")
		}
		return os.Rename(currentPath, nextPath)
	}
	if err := recoverDatabaseDeleteQuarantinesWithPublish(directory, publish); err == nil || !strings.Contains(err.Error(), "injected delete recovery publish failure") {
		t.Fatalf("recovery error = %v", err)
	}
	for _, candidate := range []string{path, path + "-wal"} {
		if db.Exists(candidate) || !db.Exists(filepath.Join(quarantineDir, filepath.Base(candidate))) {
			t.Fatalf("failed recovery left a partially restored artifact set for %q", candidate)
		}
	}
}

func TestDeleteRecoveryResumesManifestedPartialPublish(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "manifest-recovery.db")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"manifested")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	names := []string{filepath.Base(path), filepath.Base(path) + "-wal"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(quarantineDir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := json.Marshal(databaseDeleteManifest{Version: 1, Files: names})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	published := 0
	publish := func(currentPath, nextPath string) error {
		published++
		if published == 2 {
			return errors.New("injected recovery publish failure")
		}
		return os.Rename(currentPath, nextPath)
	}
	if err := recoverDatabaseDeleteQuarantinesWithPublish(directory, publish); err == nil || !strings.Contains(err.Error(), "injected recovery publish failure") {
		t.Fatalf("first recovery error = %v", err)
	}
	if !db.Exists(path) || !db.Exists(filepath.Join(quarantineDir, filepath.Base(path)+"-wal")) {
		t.Fatal("manifested recovery did not preserve its mixed durable state")
	}
	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("resume manifested recovery: %v", err)
	}
	for _, candidate := range []string{path, path + "-wal"} {
		if !db.Exists(candidate) {
			t.Fatalf("resumed recovery did not restore %q", candidate)
		}
	}
	if db.Exists(quarantineDir) {
		t.Fatal("resumed recovery retained completed quarantine")
	}
}

func TestDeleteRecoveryRemovesInterruptedHardLinkPublication(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "hard-link-recovery.db")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"hard-link")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if err := os.Link(path, filepath.Join(quarantineDir, name)); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(databaseDeleteManifest{Version: 2, Primary: name, Files: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover interrupted delete publication: %v", err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "database" {
		t.Fatalf("recovered database content = %q, err=%v", content, err)
	}
	if _, err := os.Lstat(quarantineDir); !os.IsNotExist(err) {
		t.Fatalf("recovered quarantine remains: %v", err)
	}
}

func TestDeleteRecoveryRejectsSymlinkedCompletionMarker(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "marker-symlink.db")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"marker-symlink")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if err := os.WriteFile(filepath.Join(quarantineDir, name), []byte("recoverable"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(databaseDeleteManifest{Version: 2, Primary: name, Files: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	outsideMarker := filepath.Join(directory, "outside-marker")
	if err := os.WriteFile(outsideMarker, []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideMarker, filepath.Join(quarantineDir, databaseDeleteCompleteMarker)); err != nil {
		t.Fatal(err)
	}

	err = recoverDatabaseDeleteQuarantines(directory)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlinked completion marker error=%v", err)
	}
	if !databaseCatalogFileExists(filepath.Join(quarantineDir, name)) {
		t.Fatal("malicious completion marker removed recoverable data")
	}
}

func TestDeleteRecoveryRestoresImportPrimaryFromManifest(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "pending.db.import")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"import")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if err := os.WriteFile(filepath.Join(quarantineDir, name), []byte("recoverable import"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(databaseDeleteManifest{Version: 2, Primary: name, Files: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover import quarantine: %v", err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "recoverable import" {
		t.Fatalf("import recovery content=%q err=%v", content, err)
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
	if db.Exists(filepath.Join(quarantines[0], databaseDeleteCompleteMarker)) {
		t.Fatal("failed rollback retained a completion marker that could destroy recoverable data")
	}
	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover durability rollback quarantine: %v", err)
	}
	if !db.Exists(path) {
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
	if len(items) != 1 || !db.Exists(defaultPath) || !db.Exists(defaultPath+"-wal") {
		t.Fatalf("interrupted delete was not rolled back: items=%#v", items)
	}
	if db.Exists(quarantineDir) {
		t.Fatalf("recovered quarantine directory should be removed")
	}
}

func TestDatabaseCatalogRemovesEmptyInterruptedDeleteQuarantines(t *testing.T) {
	for _, marker := range []string{"", "invalid"} {
		t.Run(marker, func(t *testing.T) {
			directory := t.TempDir()
			quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+marker)
			if err := os.Mkdir(quarantineDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if marker != "" {
				if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteCompleteMarker), []byte("incomplete"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
				t.Fatalf("recover empty quarantine: %v", err)
			}
			if db.Exists(quarantineDir) {
				t.Fatal("empty interrupted quarantine should be removed")
			}
		})
	}
}

func TestDatabaseCatalogRecoversDeleteQuarantineWithInvalidCompletionMarker(t *testing.T) {
	for _, marker := range []string{"", "complete"} {
		t.Run(fmt.Sprintf("marker_%q", marker), func(t *testing.T) {
			directory := t.TempDir()
			databasePath := filepath.Join(directory, "recoverable.db")
			quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"invalid-marker")
			if err := os.Mkdir(quarantineDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(quarantineDir, filepath.Base(databasePath)), []byte("preserved"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(quarantineDir, databaseDeleteCompleteMarker), []byte(marker), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
				t.Fatalf("recover invalid completion marker: %v", err)
			}
			contents, err := os.ReadFile(databasePath)
			if err != nil || string(contents) != "preserved" {
				t.Fatalf("recoverable database contents=%q err=%v", contents, err)
			}
			if db.Exists(quarantineDir) {
				t.Fatal("recovered quarantine directory should be removed")
			}
		})
	}
}

func TestDeleteRecoveryDoesNotPartiallyRestoreConflictingArtifactSet(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "workspace.db")
	quarantineDir := filepath.Join(directory, databaseDeleteQuarantinePrefix+"conflict")
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Base(databasePath), filepath.Base(databasePath) + "-wal"} {
		if err := os.WriteFile(filepath.Join(quarantineDir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(databasePath+"-wal", []byte("conflict"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverDatabaseDeleteQuarantines(directory); err != nil {
		t.Fatalf("recover conflict: %v", err)
	}
	if db.Exists(databasePath) || !db.Exists(filepath.Join(quarantineDir, filepath.Base(databasePath))) {
		t.Fatal("conflicting delete recovery partially restored the database")
	}
}
