//go:build windows

package databasecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsDatabaseCatalogDurabilityPaths(t *testing.T) {
	directory := t.TempDir()
	filePath := filepath.Join(directory, "database.db")
	if err := os.WriteFile(filePath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncDatabaseDeletePath(filePath); err != nil {
		t.Fatalf("sync file: %v", err)
	}
	if err := syncDatabaseDeletePath(directory); err != nil {
		t.Fatalf("sync directory: %v", err)
	}
}

func TestWindowsDatabasePathSyncFailuresAreReported(t *testing.T) {
	want := errors.New("injected failure")
	filePath := filepath.Join(t.TempDir(), "database.db")
	if err := os.WriteFile(filePath, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	ops := windowsPathSyncOps{
		stat: func(string) (os.FileInfo, error) { return info, nil },
		create: func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
			return windows.Handle(1), nil
		},
		flush: func(windows.Handle) error { return nil },
		close: func(windows.Handle) error { return nil },
	}
	if err := syncDatabaseDeletePathWindows("invalid\x00path", ops); err == nil {
		t.Fatal("path containing NUL was accepted")
	}
	ops.stat = func(string) (os.FileInfo, error) { return nil, want }
	if err := syncDatabaseDeletePathWindows(filePath, ops); !errors.Is(err, want) {
		t.Fatalf("stat failure = %v", err)
	}
	ops.stat = func(string) (os.FileInfo, error) { return info, nil }
	ops.create = func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
		return windows.InvalidHandle, want
	}
	if err := syncDatabaseDeletePathWindows(filePath, ops); !errors.Is(err, want) {
		t.Fatalf("create failure = %v", err)
	}
	ops.create = func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
		return windows.Handle(1), nil
	}
	ops.flush = func(windows.Handle) error { return want }
	if err := syncDatabaseDeletePathWindows(filePath, ops); !errors.Is(err, want) {
		t.Fatalf("flush failure = %v", err)
	}
	ops.flush = func(windows.Handle) error { return nil }
	ops.close = func(windows.Handle) error { return want }
	if err := syncDatabaseDeletePathWindows(filePath, ops); !errors.Is(err, want) {
		t.Fatalf("close failure = %v", err)
	}
}

func TestWindowsMoveAndDeleteDatabaseEndToEnd(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	target := filepath.Join(directory, "target.db")
	if err := os.WriteFile(source, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MoveDatabase(source, target); err != nil {
		t.Fatalf("move database: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
	if err := DeleteDatabase(target); err != nil {
		t.Fatalf("delete database: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists after delete: %v", err)
	}
}
