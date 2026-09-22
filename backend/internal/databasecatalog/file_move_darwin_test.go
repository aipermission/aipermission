//go:build darwin

package databasecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"golang.org/x/sys/unix"
)

func TestMoveFileNoReplaceDarwinContract(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source")
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(source, target); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "source" {
		t.Fatalf("target content = %q, err=%v", content, err)
	}
	if err := os.WriteFile(source, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(source, target); !errors.Is(err, db.ErrPublishTargetExists) {
		t.Fatalf("existing target error = %v, want %v", err, db.ErrPublishTargetExists)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "second" {
		t.Fatalf("conflicting source content = %q, err=%v", content, err)
	}
}

func TestDarwinMoveFileFailuresAreReported(t *testing.T) {
	renameErr := errors.New("rename failed")
	calls := 0
	ops := darwinMoveOps{renameExclusive: func(fromFD int, source string, toFD int, target string, flags uint32) error {
		calls++
		if fromFD != unix.AT_FDCWD || toFD != unix.AT_FDCWD || source != "source" || target != "target" || flags != unix.RENAME_EXCL {
			t.Fatalf("unexpected rename arguments: %d %q %d %q %d", fromFD, source, toFD, target, flags)
		}
		if calls == 1 {
			return unix.EEXIST
		}
		if calls == 2 {
			return renameErr
		}
		return nil
	}}
	if err := renameFileNoReplaceDarwin("source", "target", ops); !errors.Is(err, db.ErrPublishTargetExists) {
		t.Fatalf("existing target error = %v", err)
	}
	if err := renameFileNoReplaceDarwin("source", "target", ops); !errors.Is(err, renameErr) {
		t.Fatalf("rename error = %v, want %v", err, renameErr)
	}
	if err := renameFileNoReplaceDarwin("source", "target", ops); err != nil {
		t.Fatalf("successful injected move: %v", err)
	}
}
