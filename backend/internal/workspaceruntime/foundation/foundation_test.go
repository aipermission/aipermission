package foundation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

func TestInitializationErrorIncludesLatestMigrationSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.aipdb")
	older := path + ".pre-migration-v18-20260101T000000.000000000Z.aipdb"
	latest := path + ".pre-migration-v19-20260904T010000.000000000Z.aipdb"
	if err := os.WriteFile(older, []byte("snapshot"), 0o600); err != nil {
		t.Fatalf("write old snapshot fixture: %v", err)
	}
	before := preMigrationSnapshotSet(path)
	if err := os.WriteFile(latest, []byte("snapshot"), 0o600); err != nil {
		t.Fatalf("write new snapshot fixture: %v", err)
	}
	err := initializationError(path, errors.New("rewrite failed"), before)
	if !errors.Is(err, workspacelifecycle.ErrInitialization) || !strings.Contains(err.Error(), latest) {
		t.Fatalf("initialization error = %v", err)
	}
}
