package transferruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestWorkspaceCleanupExpiresPendingMCPUploadsAndKeepsLiveFiles(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertTransferRuntime(t, database)
	if _, err := database.Exec(`UPDATE connector_runtime_surfaces SET capability_kind = 'file_transfer' WHERE id = ?`, runtimeID); err != nil {
		t.Fatal(err)
	}
	store := filetransfer.NewStore(database)
	runtime := &Runtime{storageID: "workspace", store: store}
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data", "database.aipdb"), TempTTL: time.Hour})
	for _, state := range []string{filetransfer.StatusPending, filetransfer.StatusRunning} {
		localPath, size, digest, err := runner.StageUploadFile(runtime, strings.NewReader("fixture"))
		if err != nil {
			t.Fatal(err)
		}
		manifest := filetransfer.UploadManifest{FileName: "release", RemotePath: "/tmp/release", SizeBytes: size, ChecksumSHA256: digest}
		item, _, err := store.CreateMCPUpload(t.Context(), filetransfer.MCPUploadStage{
			TokenID: 1, IdempotencyKey: state, RuntimeID: runtimeID, TempPath: localPath,
			Identity: filetransfer.UploadIdentity{TargetRef: "ssh:1:1", ActionName: "upload_file", Manifest: manifest},
		}, time.Now().Add(-filetransfer.MCPUploadTTL-time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if state == filetransfer.StatusRunning {
			if started, err := store.MarkRunning(t.Context(), item.ID); err != nil || !started {
				t.Fatalf("start: %v", err)
			}
		}
		if err := runner.RecoverTempCleanup(t.Context(), runtime); err != nil {
			t.Fatal(err)
		}
		stored, err := store.Get(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(localPath)
		if state == filetransfer.StatusPending {
			if stored.Status != filetransfer.StatusCanceled || stored.TempPath != "" || !os.IsNotExist(statErr) {
				t.Fatalf("expired staging retained: status=%s path=%q stat=%v", stored.Status, stored.TempPath, statErr)
			}
		} else if stored.Status != filetransfer.StatusRunning || stored.TempPath != localPath || statErr != nil {
			t.Fatalf("running staging changed: status=%s path=%q stat=%v", stored.Status, stored.TempPath, statErr)
		}
	}
}
