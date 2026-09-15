package db

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenEncryptedProtectsDatabaseFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are verified separately")
	}
	directory := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(directory, "private.aipdb")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, directory, 0o700)
	assertPrivateMode(t, path, 0o600)
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.WriteFile(path+suffix, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path+suffix, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := preparePrivateDatabasePath(path); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, directory, 0o755)
	assertPrivateMode(t, path, 0o600)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		assertPrivateMode(t, path+suffix, 0o600)
		if err := os.Remove(path + suffix); err != nil {
			t.Fatal(err)
		}
	}
	database, err = OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
}

func TestPreparePrivateDatabasePathDoesNotChmodSharedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are verified separately")
	}
	directory := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	err := preparePrivateDatabasePath(filepath.Join(directory, "aipermission.db"))
	if err == nil {
		t.Fatal("expected group-writable parent to be rejected")
	}
	assertPrivateMode(t, directory, 0o777)
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
