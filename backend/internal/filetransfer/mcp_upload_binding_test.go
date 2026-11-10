package filetransfer

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestMCPUploadStagedPathHasExactlyOneOwner(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release", RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "exclusive"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	for index, tokenID := range []int64{1, 1, 2} {
		key := "first"
		if index > 0 {
			key = "second"
		}
		_, _, err = store.CreateMCPUpload(t.Context(), testUploadStage(request, tokenID, key), time.Now())
		if index == 0 && err != nil {
			t.Fatal(err)
		}
		if index > 0 && err == nil {
			t.Fatal("a second upload acquired an existing staged file")
		}
	}
	assertRowCount(t, database, "file_transfers", 1)
	assertRowCount(t, database, "file_transfer_start_idempotency", 1)
}

func TestMCPUploadRejectsAliasesAndRuntimeMisbinding(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), FileName: "release",
		RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "release"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	stage := testUploadStage(request, 1, "bound-upload")
	alias := stage
	alias.TempPath = filepath.Dir(stage.TempPath) + string(filepath.Separator) + "." + string(filepath.Separator) + "release"
	if _, _, err := store.CreateMCPUpload(t.Context(), alias, time.Now()); err == nil {
		t.Fatal("noncanonical staging alias accepted")
	}
	otherRuntime := insertUploadTestRuntime(t, database)
	wrongRuntime := stage
	wrongRuntime.RuntimeID = otherRuntime
	if _, _, err := store.CreateMCPUpload(t.Context(), wrongRuntime, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong target runtime accepted: %v", err)
	}
	assertRowCount(t, database, "file_transfers", 0)
	assertRowCount(t, database, "file_transfer_start_idempotency", 0)
	item, _, err := store.CreateMCPUpload(t.Context(), stage, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE file_transfers SET runtime_id = ? WHERE id = ?`, otherRuntime, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMCPUpload(t.Context(), stage.TokenID, stage.IdempotencyKey, stage.Identity); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup returned a rebound upload: %v", err)
	}
}

func TestMCPUploadLookupRejectsManifestDrift(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), FileName: "release",
		RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "release"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	stage := testUploadStage(request, 1, "immutable-upload")
	if _, _, err := store.CreateMCPUpload(t.Context(), stage, time.Now()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*UploadIdentity){
		"target":    func(i *UploadIdentity) { i.TargetRef = "ssh:2:2" },
		"action":    func(i *UploadIdentity) { i.ActionName = "other" },
		"overwrite": func(i *UploadIdentity) { i.Manifest.Overwrite = true },
		"filename":  func(i *UploadIdentity) { i.Manifest.FileName = "other" },
		"path":      func(i *UploadIdentity) { i.Manifest.RemotePath = "/tmp/other" },
		"size":      func(i *UploadIdentity) { i.Manifest.SizeBytes++ },
		"digest":    func(i *UploadIdentity) { i.Manifest.ChecksumSHA256 = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			identity := stage.Identity
			mutate(&identity)
			if _, err := store.GetMCPUpload(t.Context(), stage.TokenID, stage.IdempotencyKey, identity); !errors.Is(err, ErrIdempotencyConflict) {
				t.Fatalf("changed identity accepted: %v", err)
			}
		})
	}
}

func TestMCPUploadExpiryPreservesFractionalTime(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release", RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "fractional"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	now := time.Date(2026, 11, 11, 9, 0, 0, 900000001, time.UTC)
	item, _, err := store.CreateMCPUpload(t.Context(), testUploadStage(request, 1, "fractional"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireMCPUploads(t.Context(), now.Add(MCPUploadTTL-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), item.ID)
	if err != nil || current.Status != StatusPending {
		t.Fatalf("full TTL was not preserved: status=%s err=%v", current.Status, err)
	}
	if err := store.ExpireMCPUploads(t.Context(), now.Add(MCPUploadTTL)); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(t.Context(), item.ID)
	if err != nil || current.Status != StatusCanceled {
		t.Fatalf("upload did not expire at exact deadline: status=%s err=%v", current.Status, err)
	}
}
