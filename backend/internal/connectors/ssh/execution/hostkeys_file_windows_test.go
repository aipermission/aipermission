//go:build windows

package execution

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestKnownHostsAtomicReplacementWindowsContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeKnownHostsAtomically(path, []byte("replacement\n"), 0o600, nil); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "replacement\n" {
		t.Fatalf("known_hosts content = %q, err=%v", content, err)
	}
	if err := syncKnownHostsDirectory(filepath.Dir(path)); err != nil {
		t.Fatalf("sync Windows known_hosts directory: %v", err)
	}
}

func TestWindowsKnownHostsReplacementFailuresAreReported(t *testing.T) {
	moveErr := errors.New("move failed")
	ops := windowsKnownHostsOps{move: func(_, _ *uint16, flags uint32) error {
		if want := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH); flags != want {
			t.Fatalf("move flags = %d, want %d", flags, want)
		}
		return moveErr
	}}
	if err := renameKnownHostsFileWindows("source", "target", ops); !errors.Is(err, moveErr) {
		t.Fatalf("move error = %v, want %v", err, moveErr)
	}
	if err := renameKnownHostsFileWindows("source\x00invalid", "target", ops); err == nil {
		t.Fatal("invalid source path was accepted")
	}
	if err := renameKnownHostsFileWindows("source", "target\x00invalid", ops); err == nil {
		t.Fatal("invalid target path was accepted")
	}
	if err := renameKnownHostsFileWindows("source", "target", windowsKnownHostsOps{
		move: func(_, _ *uint16, _ uint32) error { return nil },
	}); err != nil {
		t.Fatalf("successful injected move: %v", err)
	}
}
