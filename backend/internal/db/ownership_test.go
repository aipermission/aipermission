package db

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDatabaseOwnershipIsExclusiveAndReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	first, err := AcquireDatabaseOwnership(path)
	if err != nil {
		t.Fatalf("acquire first ownership: %v", err)
	}

	if _, err := AcquireDatabaseOwnership(path); !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("second ownership error = %v, want ErrDatabaseInUse", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first ownership: %v", err)
	}

	second, err := AcquireDatabaseOwnership(path)
	if err != nil {
		t.Fatalf("reacquire released ownership: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("release second ownership: %v", err)
	}
}

func TestDatabaseOwnershipIsExclusiveAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	ownership, err := AcquireDatabaseOwnership(path)
	if err != nil {
		t.Fatalf("acquire parent ownership: %v", err)
	}
	runDatabaseOwnershipHelper(t, path, true)
	if err := ownership.Close(); err != nil {
		t.Fatalf("release parent ownership: %v", err)
	}
	runDatabaseOwnershipHelper(t, path, false)
}

func TestDatabaseOwnershipHelperProcess(t *testing.T) {
	if os.Getenv("AIPERMISSION_OWNERSHIP_HELPER") != "1" {
		return
	}
	path := os.Getenv("AIPERMISSION_OWNERSHIP_PATH")
	wantConflict := os.Getenv("AIPERMISSION_OWNERSHIP_CONFLICT") == "1"
	ownership, err := AcquireDatabaseOwnership(path)
	if wantConflict {
		if !errors.Is(err, ErrDatabaseInUse) {
			t.Fatalf("child ownership error = %v, want ErrDatabaseInUse", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("child ownership after release: %v", err)
	}
	if err := ownership.Close(); err != nil {
		t.Fatalf("release child ownership: %v", err)
	}
}

func runDatabaseOwnershipHelper(t *testing.T, path string, wantConflict bool) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDatabaseOwnershipHelperProcess$")
	command.Env = append(os.Environ(),
		"AIPERMISSION_OWNERSHIP_HELPER=1",
		"AIPERMISSION_OWNERSHIP_PATH="+path,
		fmt.Sprintf("AIPERMISSION_OWNERSHIP_CONFLICT=%d", boolInt(wantConflict)),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ownership helper failed: %v\n%s", err, output)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
