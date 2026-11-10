package filetransfer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestUploadManifestRejectsUnsafeOrAmbiguousMetadata(t *testing.T) {
	valid := UploadManifest{FileName: "release.tar.gz", RemotePath: "/tmp/release.tar.gz", SizeBytes: 4, ChecksumSHA256: strings.Repeat("a", 64)}
	for name, mutate := range map[string]func(*UploadManifest){
		"filename traversal": func(m *UploadManifest) { m.FileName = "../secret" },
		"empty path":         func(m *UploadManifest) { m.RemotePath = "" },
		"negative size":      func(m *UploadManifest) { m.SizeBytes = -1 },
		"oversized":          func(m *UploadManifest) { m.SizeBytes = MaxMCPUploadBytes + 1 },
		"missing digest":     func(m *UploadManifest) { m.ChecksumSHA256 = "" },
		"uppercase digest":   func(m *UploadManifest) { m.ChecksumSHA256 = strings.Repeat("A", 64) },
		"invalid digest":     func(m *UploadManifest) { m.ChecksumSHA256 = strings.Repeat("z", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := valid
			mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
	valid.SizeBytes = 0
	if err := valid.Validate(); err != nil {
		t.Fatalf("empty file is valid: %v", err)
	}
}

func TestMCPUploadOwnershipIdempotencyQuotaAndExpiry(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtimeID := insertUploadTestRuntime(t, database)
	store := NewStore(database)
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("fixture"))
	request := CreateRequest{
		RuntimeID: runtimeID, Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release.tar.gz", LocalPath: "release.tar.gz", RemotePath: "/tmp/release.tar.gz",
		SizeBytes: MaxMCPUploadBytes, ChecksumSHA256: hex.EncodeToString(digest[:]), TempPath: filepath.Join(t.TempDir(), "upload-fixture"),
	}
	stage := testUploadStage(request, 7, "upload-one")
	first, created, err := store.CreateMCPUpload(t.Context(), stage, now)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	if first.TempExpiresAt != now.Add(MCPUploadTTL).Format(mcpUploadTimeLayout) {
		t.Fatalf("unexpected expiry: %s", first.TempExpiresAt)
	}
	if _, err := store.GetMCPUpload(t.Context(), 7, stage.IdempotencyKey, stage.Identity); err != nil {
		t.Fatal(err)
	}
	for _, tokenID := range []int64{0, 8} {
		if _, err := store.GetMCPUpload(t.Context(), tokenID, stage.IdempotencyKey, stage.Identity); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-token lookup leaked upload: %v", err)
		}
	}
	replay, created, err := store.CreateMCPUpload(t.Context(), stage, now)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay: created=%v id=%d err=%v", created, replay.ID, err)
	}
	drift := stage
	drift.Identity.Manifest.Overwrite = true
	if _, _, err := store.CreateMCPUpload(t.Context(), drift, now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("drift not rejected: %v", err)
	}
	stage.IdempotencyKey = "upload-two"
	stage.TempPath = filepath.Join(t.TempDir(), "upload-second")
	second, _, err := store.CreateMCPUpload(t.Context(), stage, now)
	if err != nil {
		t.Fatal(err)
	}
	stage.IdempotencyKey = "upload-three"
	stage.TempPath = filepath.Join(t.TempDir(), "upload-third")
	stage.Identity.Manifest.SizeBytes = 1
	if _, _, err := store.CreateMCPUpload(t.Context(), stage, now); !errors.Is(err, ErrMCPUploadCapacity) {
		t.Fatalf("byte quota not enforced: %v", err)
	}
	if started, err := store.MarkRunning(t.Context(), second.ID); err != nil || !started {
		t.Fatalf("mark running: %v", err)
	}
	if err := store.ExpireMCPUploads(t.Context(), now.Add(MCPUploadTTL-time.Second)); err != nil {
		t.Fatal(err)
	}
	item, _ := store.Get(t.Context(), first.ID)
	if item.Status != StatusPending {
		t.Fatal("upload expired early")
	}
	if err := store.ExpireMCPUploads(t.Context(), now.Add(MCPUploadTTL)); err != nil {
		t.Fatal(err)
	}
	for id, expected := range map[int64]string{first.ID: StatusCanceled, second.ID: StatusRunning} {
		item, err := store.Get(t.Context(), id)
		if err != nil || item.Status != expected {
			t.Fatalf("expiry affected wrong state: id=%d status=%s err=%v", id, item.Status, err)
		}
		var historyStatus string
		if err := database.QueryRowContext(t.Context(), `SELECT status FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, id).Scan(&historyStatus); err != nil || historyStatus != expected {
			t.Fatalf("history drift: status=%s err=%v", historyStatus, err)
		}
	}
}

func TestConcurrentMCPUploadQuotaCannotBeOverbooked(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release", RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "release"), SizeBytes: MaxMCPUploadBytes, ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for index := 0; index < cap(results); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			candidate := request
			candidate.TempPath = filepath.Join(filepath.Dir(request.TempPath), fmt.Sprintf("concurrent-%d", index))
			_, _, err := store.CreateMCPUpload(t.Context(), testUploadStage(candidate, int64(index+1), "concurrent"), time.Now())
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	var admitted int
	for err := range results {
		if err == nil {
			admitted++
		} else if !errors.Is(err, ErrMCPUploadCapacity) {
			t.Fatal(err)
		}
	}
	if admitted != 2 {
		t.Fatalf("quota admitted %d uploads, expected exactly two", admitted)
	}
	assertRowCount(t, database, "file_transfers", 2)
	assertRowCount(t, database, "file_transfer_start_idempotency", 2)
}

func TestMCPUploadFileCountQuotaIncludesEmptyFiles(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "empty", RemotePath: "/tmp/empty", TempPath: filepath.Join(t.TempDir(), "empty"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	for index := 0; index <= MaxMCPUploadStagedFiles; index++ {
		request.TempPath = filepath.Join(filepath.Dir(request.TempPath), fmt.Sprintf("empty-%d", index))
		_, _, err = store.CreateMCPUpload(t.Context(), testUploadStage(request, 1, fmt.Sprintf("empty-%d", index)), time.Now())
		if index < MaxMCPUploadStagedFiles && err != nil {
			t.Fatal(err)
		}
		if index == MaxMCPUploadStagedFiles && !errors.Is(err, ErrMCPUploadCapacity) {
			t.Fatalf("empty file quota not enforced: %v", err)
		}
	}
}

func testUploadIdentity(request CreateRequest) UploadIdentity {
	return UploadIdentity{TargetRef: "ssh:1:1", ActionName: "upload_file", Manifest: UploadManifest{
		FileName: request.FileName, RemotePath: request.RemotePath, SizeBytes: request.SizeBytes, ChecksumSHA256: request.ChecksumSHA256,
	}}
}

func testUploadStage(request CreateRequest, tokenID int64, key string) MCPUploadStage {
	return MCPUploadStage{TokenID: tokenID, IdempotencyKey: key, Identity: testUploadIdentity(request), RuntimeID: request.RuntimeID, TempPath: request.TempPath}
}

func insertUploadTestRuntime(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	id := insertTestServer(t, database)
	if _, err := database.Exec(`UPDATE connector_targets SET name = 'upload-fixture-' || id
		WHERE id = (SELECT target_id FROM connector_runtime_surfaces WHERE id = ?)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE connector_runtime_surfaces SET capability_kind = 'file_transfer' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	return id
}
