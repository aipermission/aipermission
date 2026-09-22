//go:build windows

package databasecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"golang.org/x/sys/windows"
)

func TestMoveFileNoReplaceWindowsContract(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.aipdb")
	targetPath := filepath.Join(directory, "target.aipdb")

	if err := moveFileNoReplace(sourcePath+"\x00", targetPath); err == nil {
		t.Fatal("source path containing NUL was accepted")
	}

	if err := os.WriteFile(sourcePath, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(sourcePath, targetPath+"\x00"); err == nil {
		t.Fatal("target path containing NUL was accepted")
	}
	if err := os.WriteFile(targetPath, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(sourcePath, targetPath); !errors.Is(err, db.ErrPublishTargetExists) {
		t.Fatalf("move conflict = %v", err)
	}

	if err := os.Remove(targetPath); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(filepath.Join(directory, "missing.aipdb"), targetPath); err == nil {
		t.Fatal("missing source was accepted")
	}
	if err := moveFileNoReplace(sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("move source still exists: %v", err)
	}
	if content, err := os.ReadFile(targetPath); err != nil || string(content) != "candidate" {
		t.Fatalf("moved target = %q err=%v", content, err)
	}
}

func TestWindowsMoveFileFailuresAreReported(t *testing.T) {
	want := errors.New("injected failure")
	ops := nativeWindowsMoveOps()
	if err := renameFileNoReplaceWindows("source\x00", "target", ops); err == nil {
		t.Fatal("move source path containing NUL was accepted")
	}
	if err := renameFileNoReplaceWindows("source", "target\x00", ops); err == nil {
		t.Fatal("move target path containing NUL was accepted")
	}
	ops.create = func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
		return windows.InvalidHandle, want
	}
	if err := syncFileForMoveWindows("candidate", ops); !errors.Is(err, want) {
		t.Fatalf("create failure = %v", err)
	}

	ops.create = func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
		return windows.Handle(1), nil
	}
	ops.flush = func(windows.Handle) error { return want }
	ops.close = func(windows.Handle) error { return nil }
	if err := syncFileForMoveWindows("candidate", ops); !errors.Is(err, want) {
		t.Fatalf("flush failure = %v", err)
	}

	ops.flush = func(windows.Handle) error { return nil }
	ops.close = func(windows.Handle) error { return want }
	if err := syncFileForMoveWindows("candidate", ops); !errors.Is(err, want) {
		t.Fatalf("close failure = %v", err)
	}

	ops.move = func(*uint16, *uint16, uint32) error { return want }
	if err := renameFileNoReplaceWindows("source", "target", ops); !errors.Is(err, want) {
		t.Fatalf("move failure = %v", err)
	}
	if err := syncMoveDirectories("source", "target"); err != nil {
		t.Fatalf("Windows directory sync = %v", err)
	}
}
