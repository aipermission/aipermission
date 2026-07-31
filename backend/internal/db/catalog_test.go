package db

import (
	"os"
	"path/filepath"
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
