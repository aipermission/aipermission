package api

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestRetentionSettingsSaveAndPurgeOldRecords(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	connectorStore := connectortargets.NewStore(fixture.db)
	connectorRequest, err := connectorStore.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             server.TargetID,
		ProfileID:            server.ProfileID,
		ConnectorKind:        "ssh",
		ActionName:           "read_console",
		Source:               "mcp",
		Input:                map[string]any{"tail_bytes": 100},
		EncryptedPayloadJSON: "encrypted",
		Reason:               "old connector action",
		Status:               connectors.ResultCompleted,
	})
	if err != nil {
		t.Fatalf("insert old connector request: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET created_at = ?, completed_at = ? WHERE id = ?`, old, old, connectorRequest.ID); err != nil {
		t.Fatalf("age connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(context.Background(), connectorRequest.ID); err != nil {
		t.Fatalf("sync old connector history: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, runtime_id, action, payload_json, created_at)
		VALUES ('user', ?, ?, 'old.audit', '{}', ?)`,
		token.ID,
		server.ID,
		old,
	); err != nil {
		t.Fatalf("insert old audit log: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
		VALUES (?, 'old console', 'closed', 'old transcript', 120, 32, ?, ?, ?)`,
		server.ID,
		old,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old console session: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO message_queue (token_id, runtime_id, direction, message, consumed_at, created_at)
		VALUES (?, ?, 'user_to_ai', 'old message', ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old message: %v", err)
	}

	updateResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/retention", "", updateRetentionSettingsRequest{
		HistoryDays: 7,
		AuditDays:   7,
		ConsoleDays: 7,
		MessageDays: 7,
	})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update retention failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	if !strings.Contains(updateResponse.Body.String(), `"history_days":7`) {
		t.Fatalf("retention response missing saved value: %s", updateResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 0)
	assertTableCount(t, fixture.db, "connector_action_requests", 0)
	assertTableCount(t, fixture.db, "history_entries", 0)
	assertTableCount(t, fixture.db, "console_sessions", 0)
	assertTableCount(t, fixture.db, "message_queue", 0)
	var settingsAuditCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'settings.retention.updated'`).Scan(&settingsAuditCount); err != nil {
		t.Fatal(err)
	}
	if settingsAuditCount != 1 {
		t.Fatalf("retention settings audit count = %d, want 1", settingsAuditCount)
	}

	purgeResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "audit", Days: 0})
	if purgeResponse.Code != http.StatusBadRequest {
		t.Fatalf("manual purge should reject zero days, got %d %s", purgeResponse.Code, purgeResponse.Body.String())
	}
}

func TestRetentionDisabledKeepsOldRecordsAndManualPurgeDeletes(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, runtime_id, action, payload_json, created_at)
		VALUES ('user', ?, ?, 'old.audit', '{}', ?)`,
		token.ID,
		server.ID,
		old,
	); err != nil {
		t.Fatalf("insert old audit log: %v", err)
	}

	updateResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/retention", "", updateRetentionSettingsRequest{})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("disable retention failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 1)

	purgeResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "history", Days: 7})
	if purgeResponse.Code != http.StatusOK || !strings.Contains(purgeResponse.Body.String(), `"deleted":1`) {
		t.Fatalf("manual history purge failed: %d %s", purgeResponse.Code, purgeResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 0)

	badTargetResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "unknown", Days: 7})
	if badTargetResponse.Code != http.StatusBadRequest || !strings.Contains(badTargetResponse.Body.String(), "invalid retention target") {
		t.Fatalf("invalid purge target should fail: %d %s", badTargetResponse.Code, badTargetResponse.Body.String())
	}
}

func TestFileTransferRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	tempRoot := fileTransferHandlers{fixture.server}.fileTransferTempRoot()
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatalf("create temp root: %v", err)
	}
	tempPath := filepath.Join(tempRoot, "download-test.txt")
	if err := os.WriteFile(tempPath, []byte("download payload"), 0o600); err != nil {
		t.Fatalf("write download file: %v", err)
	}
	record, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID:  server.ID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceUI,
		RemotePath: "/var/log/app.log",
		FileName:   "app.log",
		TempPath:   tempPath,
	})
	if err != nil {
		t.Fatalf("create file transfer: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkRunning(context.Background(), record.ID); err != nil || !ok {
		t.Fatalf("mark file transfer running: ok=%v err=%v", ok, err)
	}
	if ok, err := runtime.fileTransfers.Complete(context.Background(), record.ID, int64(len("download payload")), "abc123"); err != nil || !ok {
		t.Fatalf("complete file transfer: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers?paginated=true&direction=download&q=app", "", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"remote_path":"/var/log/app.log"`) {
		t.Fatalf("list file transfers failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers/"+strconv.FormatInt(record.ID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || strings.Contains(detailResponse.Body.String(), "download-test.txt") || !strings.Contains(detailResponse.Body.String(), `"checksum_sha256":"abc123"`) {
		t.Fatalf("get file transfer failed or leaked temp path: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	downloadResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers/"+strconv.FormatInt(record.ID, 10)+"/download", "", nil)
	if downloadResponse.Code != http.StatusOK || downloadResponse.Body.String() != "download payload" {
		t.Fatalf("download completed transfer failed: %d %s", downloadResponse.Code, downloadResponse.Body.String())
	}

	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers?direction=copy", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid direction should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{RuntimeID: server.ID, RemotePath: "relative.txt"}); response.Code != http.StatusBadRequest {
		t.Fatalf("relative download path should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{RuntimeID: server.ID, Path: "relative"}); response.Code != http.StatusBadRequest {
		t.Fatalf("relative browse path should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/upload", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("missing multipart upload should fail, got %d %s", response.Code, response.Body.String())
	}

	cancelRecord, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID:  server.ID,
		Direction:  filetransfer.DirectionUpload,
		Source:     filetransfer.SourceUI,
		LocalPath:  "movie.mp4",
		RemotePath: "/root/movie.mp4",
		FileName:   "movie.mp4",
		TempPath:   tempPath,
	})
	if err != nil {
		t.Fatalf("create cancel transfer: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkRunning(context.Background(), cancelRecord.ID); err != nil || !ok {
		t.Fatalf("mark cancel transfer running: ok=%v err=%v", ok, err)
	}
	cancelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/"+strconv.FormatInt(cancelRecord.ID, 10)+"/cancel", "", map[string]any{})
	if cancelResponse.Code != http.StatusOK || !strings.Contains(cancelResponse.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel file transfer failed: %d %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	batch, err := runtime.fileTransfers.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: server.ID,
		Direction: filetransfer.DirectionUpload,
		Source:    filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", TempPath: tempPath},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", TempPath: tempPath},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkBatchRunning(context.Background(), batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := runtime.fileTransfers.PauseBatch(context.Background(), batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}
	batchListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfer-batches?runtime_id="+strconv.FormatInt(server.ID, 10), "", nil)
	if batchListResponse.Code != http.StatusOK {
		t.Fatalf("list file transfer batches failed: %d %s", batchListResponse.Code, batchListResponse.Body.String())
	}
	batchList := decodeRouteResponse[pageResponse[filetransfer.BatchRecord]](t, batchListResponse.Body.Bytes())
	if batchList.Total == 0 || len(batchList.Items) == 0 {
		t.Fatalf("expected batch list response to include created batch: %#v", batchList)
	}
	var listedBatch filetransfer.BatchRecord
	for _, item := range batchList.Items {
		if item.ID == batch.ID {
			listedBatch = item
			break
		}
	}
	if listedBatch.ID == 0 || len(listedBatch.Items) != 2 {
		t.Fatalf("batch list should include per-file items, got %#v", listedBatch)
	}
	duplicateQueueResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfer-batches/"+strconv.FormatInt(batch.ID, 10)+"/queue", "", map[string]any{
		"item_ids": []int64{batch.Items[0].ID, batch.Items[0].ID},
	})
	if duplicateQueueResponse.Code != http.StatusBadRequest {
		t.Fatalf("duplicate queue item ids should fail with 400, got %d %s", duplicateQueueResponse.Code, duplicateQueueResponse.Body.String())
	}
}

func assertTableCount(t *testing.T, database *sql.DB, table string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != expected {
		t.Fatalf("unexpected %s count: got %d want %d", table, count, expected)
	}
}

func insertRouteCommandRequest(t *testing.T, database *sql.DB, tokenID int64, runtimeID int64, status string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, created_at)
		VALUES (?, ?, 'ls', 'test reason', ?, '', '', ?)`,
		tokenID,
		runtimeID,
		status,
		now,
	)
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("command request id: %v", err)
	}
	return id
}

func insertManualRouteCommandRequest(t *testing.T, database *sql.DB, runtimeID int64, command string, reason string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, tracking_reason, stdout, stderr, created_at, completed_at)
		VALUES (?, 'manual', ?, 'manual command not tracked', 'untracked', ?, '', '', ?, ?)`,
		runtimeID,
		command,
		reason,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert manual command request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("manual command request id: %v", err)
	}
	return id
}
