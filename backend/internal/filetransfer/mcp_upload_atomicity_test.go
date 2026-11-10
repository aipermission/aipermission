package filetransfer

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestMCPUploadHistoryFailureRollsBackRecordAndClaim(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release", RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "release"), ChecksumSHA256: strings.Repeat("a", 64)}
	stage := testUploadStage(request, 1, "atomic-upload")
	if _, err := database.Exec(`CREATE TRIGGER reject_mcp_upload_history BEFORE INSERT ON history_entries
		BEGIN SELECT RAISE(ABORT, 'history unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if _, _, err := store.CreateMCPUpload(t.Context(), stage, time.Now()); err == nil {
		t.Fatal("staging without durable History accepted")
	}
	assertRowCount(t, database, "file_transfers", 0)
	assertRowCount(t, database, "file_transfer_start_idempotency", 0)
	if _, err := database.Exec(`DROP TRIGGER reject_mcp_upload_history`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.CreateMCPUpload(t.Context(), stage, time.Now()); err != nil || !created {
		t.Fatalf("rolled-back key could not be retried: created=%v err=%v", created, err)
	}
}

func TestMCPUploadExpiryHistoryFailureIsAtomicAndDoesNotTouchUI(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	request := CreateRequest{RuntimeID: insertUploadTestRuntime(t, database), Source: SourceMCP, Direction: DirectionUpload,
		FileName: "release", RemotePath: "/tmp/release", TempPath: filepath.Join(t.TempDir(), "release"), ChecksumSHA256: strings.Repeat("a", 64)}
	store := NewStore(database)
	now := time.Now().UTC().Truncate(time.Second)
	staged, _, err := store.CreateMCPUpload(t.Context(), testUploadStage(request, 1, "expiry-atomic"), now.Add(-MCPUploadTTL))
	if err != nil {
		t.Fatal(err)
	}
	request.Source = SourceUI
	ui, err := store.Create(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetTempExpiry(t.Context(), ui.ID, ui.TempPath, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_mcp_expiry_history BEFORE UPDATE OF status ON history_entries
		BEGIN SELECT RAISE(ABORT, 'history unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireMCPUploads(t.Context(), now); err == nil {
		t.Fatal("expiry without History accepted")
	}
	item, err := store.Get(t.Context(), staged.ID)
	if err != nil || item.Status != StatusPending {
		t.Fatalf("expiry left partial cancellation: item=%#v err=%v", item, err)
	}
	if _, err := database.Exec(`DROP TRIGGER reject_mcp_expiry_history`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireMCPUploads(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	item, err = store.Get(t.Context(), ui.ID)
	if err != nil || item.Status != StatusPending {
		t.Fatalf("MCP expiry affected UI upload: item=%#v err=%v", item, err)
	}
}
