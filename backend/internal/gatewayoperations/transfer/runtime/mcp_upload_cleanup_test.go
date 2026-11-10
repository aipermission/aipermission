package transferruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/tokens"
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

func TestDeclinedFreshMCPUploadsReleaseFilesAndQuota(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertTransferRuntime(t, database)
	if _, err := database.Exec(`UPDATE connector_runtime_surfaces SET capability_kind = 'file_transfer' WHERE id = ?`, runtimeID); err != nil {
		t.Fatal(err)
	}
	token, err := tokens.NewStore(database).Create(t.Context(), tokens.CreateRequest{Name: "upload-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	store := filetransfer.NewStore(database)
	runtime := &Runtime{storageID: "workspace", store: store}
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data", "database.aipdb"), TempTTL: time.Hour})
	stage := func(key string) filetransfer.MCPUploadStage {
		t.Helper()
		localPath, size, digest, err := runner.StageUploadFile(runtime, strings.NewReader("fixture"))
		if err != nil {
			t.Fatal(err)
		}
		return filetransfer.MCPUploadStage{TokenID: token.ID, IdempotencyKey: key, RuntimeID: runtimeID, TempPath: localPath,
			Identity: filetransfer.UploadIdentity{TargetRef: "ssh:1:1", ActionName: "upload_file",
				Manifest: filetransfer.UploadManifest{FileName: "release", RemotePath: "/tmp/release", SizeBytes: size, ChecksumSHA256: digest}}}
	}
	var stages []filetransfer.MCPUploadStage
	for i := range filetransfer.MaxMCPUploadStagedFiles {
		staged := stage(fmt.Sprintf("decline-%d", i))
		stages = append(stages, staged)
		if _, _, err := store.CreateMCPUpload(t.Context(), staged, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	next := stage("after-decline")
	if _, _, err := store.CreateMCPUpload(t.Context(), next, time.Now()); !errors.Is(err, filetransfer.ErrMCPUploadCapacity) {
		t.Fatalf("full staging quota: %v", err)
	}
	runner.RemoveTempPath(runtime, next.TempPath)
	for _, staged := range stages {
		_, err := connectortargets.NewStore(database).InsertActionRequest(t.Context(), connectortargets.InsertActionRequestInput{
			TokenID: &token.ID, TargetID: 1, ProfileID: 1, ConnectorKind: "ssh", ActionName: "upload_file",
			Title: "fixture action", Source: "mcp", Status: connectors.ResultDeclined,
			IdempotencyKey: staged.IdempotencyKey, IdempotencyIdentityHash: strings.Repeat("a", 64),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.RecoverTempCleanup(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	for _, staged := range stages {
		if _, err := os.Stat(staged.TempPath); !os.IsNotExist(err) {
			t.Fatalf("declined fresh file retained: %v", err)
		}
	}
	var count, size int64
	if err := database.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM file_transfers
		WHERE source = 'mcp' AND direction = 'upload' AND temp_path <> ''`).Scan(&count, &size); err != nil {
		t.Fatal(err)
	}
	if count != 0 || size != 0 {
		t.Fatalf("declined staging quota retained: files=%d bytes=%d", count, size)
	}
	next = stage("after-decline")
	if _, _, err := store.CreateMCPUpload(t.Context(), next, time.Now()); err != nil {
		t.Fatalf("reclaimed quota rejected next stage: %v", err)
	}
}
