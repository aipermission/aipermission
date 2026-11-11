package actions

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestUploadAuthorizationPreservesDatabaseWaitDeadline(t *testing.T) {
	database, err := appdb.OpenEncrypted(filepath.Join(t.TempDir(), "upload.aipdb"), "DisposableUploadPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	runtime := &Runtime{database: database}
	err = runtime.revalidateUploadDispatch(ctx, connectortargets.ActionRequest{ID: 1}, PreparedRequest{}, connectortargets.ActionPermissionAlwaysRun)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrConnectorAuthorizationChanged) {
		t.Fatalf("database contention mislabeled as permission change: %v", err)
	}
}
