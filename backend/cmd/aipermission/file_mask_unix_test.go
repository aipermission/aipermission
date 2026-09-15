//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestApplyPrivateFileCreationMaskProtectsNewFiles(t *testing.T) {
	previous := syscall.Umask(0o022)
	defer syscall.Umask(previous)

	applyPrivateFileCreationMask()
	path := filepath.Join(t.TempDir(), "private")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("created file mode = %04o, want 0600", got)
	}
}
