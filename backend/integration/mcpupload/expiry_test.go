package mcpupload

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	appdb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func (f *uploadFixture) openStore(t *testing.T) *sql.DB {
	t.Helper()
	items, err := databasecatalog.ListDatabases(f.dataPath, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("disposable upload database: %#v %v", items, err)
	}
	database, err := appdb.OpenEncrypted(items[0].Path, "DisposableFixturePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database
}

func expireUploadFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), `UPDATE file_transfers SET temp_expires_at = ? WHERE source = 'mcp' AND direction = 'upload'`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := filetransfer.NewStore(database).ExpireMCPUploads(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredOrphanUploadCannotCreateNewApproval(t *testing.T) {
	f, target := newUploadFixture(t)
	token := f.token("capacity-upload")
	f.grantUpload(target, token.ID, connectortargets.ActionPermissionApprovalRequired)
	database := f.openStore(t)
	store := connectortargets.NewStore(database)
	fixtureVault, err := vault.New("Disposable capacity fixture encryption material")
	if err != nil {
		t.Fatal(err)
	}
	// Pending requests reserve terminal space without allocating large payloads.
	for range actioncapacity.MaxBytes / actioncapacity.TerminalReservationBytes {
		if _, _, err := store.InsertSealedActionRequestIdempotent(t.Context(), connectortargets.InsertActionRequestInput{
			TokenID: &token.ID, TargetID: target.ID, ProfileID: target.Profiles[0].ID,
			ConnectorKind: "ssh", ActionName: "exec", Source: "mcp", Status: connectors.ResultApprovalPending,
			ApprovalContext: "{}", ApprovalContextHash: "fixture-only-hash",
		}, func(id int64) (string, error) {
			return recordcrypto.EncryptJSON(fixtureVault, "disposable-capacity", recordcrypto.ConnectorActionRequest, id, map[string]any{"fixture": true})
		}); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(t.TempDir(), "expired.bin")
	content := []byte("capacity retry")
	metadata := uploadTestMetadata(target.TargetRef, destination, "capacity-retry", content)
	response := sendUploadFixture(t, f.handler, token.TokenValue, metadata, content, false)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "connector_action_backpressure") || !strings.Contains(response.Body.String(), "limit=stored_bytes") {
		t.Fatalf("capacity rejection: %d %s", response.Code, response.Body)
	}
	var stageCount int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM file_transfers`).Scan(&stageCount); err != nil || stageCount != 1 {
		t.Fatalf("stage was not committed before action capacity rejection: %d %v", stageCount, err)
	}
	expireUploadFixture(t, database)
	var stagedPath string
	if err := database.QueryRowContext(t.Context(), `SELECT temp_path FROM file_transfers LIMIT 1`).Scan(&stagedPath); err != nil {
		t.Fatal(err)
	}
	stagedBefore, err := os.ReadDir(filepath.Dir(stagedPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE connector_action_requests SET status = 'completed' WHERE token_id = ?`, token.ID); err != nil {
		t.Fatal(err)
	}
	response = sendUploadFixture(t, f.handler, token.TokenValue, metadata, content, false)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"upload_stage_expired"`) {
		t.Fatalf("expired retry created an impossible approval: %d %s", response.Code, response.Body)
	}
	var approvals, stages int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM connector_action_requests WHERE action_name = 'upload_file'`).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("unexpected approval: %d %v", approvals, err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM file_transfers`).Scan(&stages); err != nil || stages != stageCount {
		t.Fatalf("retry replaced immutable stage: %d %v", stages, err)
	}
	stagedAfter, err := os.ReadDir(filepath.Dir(stagedPath))
	if err != nil || len(stagedAfter) != len(stagedBefore) {
		t.Fatalf("retry leaked a new private file: before=%v after=%v err=%v", stagedBefore, stagedAfter, err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("expired upload published: %v", err)
	}
}

func TestPollingExpiredUploadStalesOnlyOwnedPendingApproval(t *testing.T) {
	f, target := newUploadFixture(t)
	token, other := f.token("pending-upload"), f.token("other-upload")
	f.grantUpload(target, token.ID, connectortargets.ActionPermissionApprovalRequired)
	destination := filepath.Join(t.TempDir(), "pending.bin")
	data := []byte("pending expiry")
	metadata := uploadTestMetadata(target.TargetRef, destination, "pending-expiry", data)
	approval := requireUploadAction(t, sendUploadFixture(t, f.handler, token.TokenValue, metadata, data, false), connectors.ResultApprovalPending)
	database := f.openStore(t)
	expireUploadFixture(t, database)
	url := fmt.Sprintf("/api/mcp/connector-action-requests/%d", approval.RequestID)
	if foreign := performJSON(f.handler, http.MethodGet, url, other.TokenValue, nil); foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign poll: %d %s", foreign.Code, foreign.Body)
	}
	item, err := connectortargets.NewStore(database).GetActionRequest(t.Context(), approval.RequestID)
	if err != nil || item.Status != connectors.ResultApprovalPending {
		t.Fatalf("foreign poll changed approval: %#v %v", item, err)
	}
	before, err := actioncapacity.Measure(t.Context(), database, token.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		requireUploadAction(t, performJSON(f.handler, http.MethodGet, url, token.TokenValue, nil), connectors.ResultStale)
	}
	item, err = connectortargets.NewStore(database).GetActionRequest(t.Context(), approval.RequestID)
	if err != nil || item.ApprovalContextDrift != "upload_stage" || item.DispatchStartedAt != "" {
		t.Fatalf("expiry finality: %#v %v", item, err)
	}
	var auditCount int
	if err := database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'connector_action.request.stale'`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("stale audit duplicated or missing: %d %v", auditCount, err)
	}
	after, err := actioncapacity.Measure(t.Context(), database, token.ID, 0)
	if err != nil || after.Bytes >= before.Bytes {
		t.Fatalf("pending terminal reservation not released: before=%#v after=%#v err=%v", before, after, err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("expired approval published: %v", err)
	}
}
