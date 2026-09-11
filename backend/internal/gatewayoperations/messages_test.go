package gatewayoperations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/messagequeue"
)

func TestMessageHTTPHandlersPreserveStrictContract(t *testing.T) {
	database := openMessageTestDatabase(t)
	tokenID := insertMessageTestToken(t, database)
	runtimeID := insertMessageTestRuntime(t, database)
	handlers := NewMessageHTTPHandlers(func(http.ResponseWriter) (MessageScope, bool) {
		return MessageScope{Store: NewMessageStore(database, func(_ context.Context, value string) string { return value })}, true
	})

	created := performMessageRequest(t, handlers.Create, http.MethodPost, "/api/messages", messagequeue.CreateRequest{
		TokenID: tokenID, RuntimeID: &runtimeID, Message: "hello",
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"message":"hello"`) {
		t.Fatalf("create response = %d %s", created.Code, created.Body.String())
	}

	listed := httptest.NewRecorder()
	handlers.List(listed, httptest.NewRequest(http.MethodGet, "/api/messages?runtime_id="+strconv.FormatInt(runtimeID, 10), nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"message":"hello"`) {
		t.Fatalf("list response = %d %s", listed.Code, listed.Body.String())
	}

	invalid := httptest.NewRecorder()
	handlers.List(invalid, httptest.NewRequest(http.MethodGet, "/api/messages?runtime_id=nope", nil))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid runtime_id") {
		t.Fatalf("invalid filter response = %d %s", invalid.Code, invalid.Body.String())
	}

	unknown := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(`{"token_id":1,"message":"hello","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.Create(unknown, request)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "invalid json body") {
		t.Fatalf("unknown field response = %d %s", unknown.Code, unknown.Body.String())
	}

	if _, err := database.Exec(`UPDATE message_queue SET direction = 'ai_to_user'`); err != nil {
		t.Fatalf("prepare unread reply: %v", err)
	}
	read := performMessageRequest(t, handlers.MarkRead, http.MethodPost, "/api/messages/read", map[string]any{"runtime_id": runtimeID})
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"status":"read"`) || !strings.Contains(read.Body.String(), `"count":1`) {
		t.Fatalf("mark read response = %d %s", read.Code, read.Body.String())
	}
}

func TestMessageBoundaryRejectsIncompleteStores(t *testing.T) {
	if NewMessageStore(nil, func(_ context.Context, value string) string { return value }) != nil {
		t.Fatal("message store accepted a nil database")
	}
	if NewMessageStore(&sql.DB{}, nil) != nil {
		t.Fatal("message store accepted a nil redactor")
	}

	handlers := NewMessageHTTPHandlers(func(http.ResponseWriter) (MessageScope, bool) {
		return MessageScope{}, true
	})
	response := httptest.NewRecorder()
	handlers.List(response, httptest.NewRequest(http.MethodGet, "/api/messages", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("incomplete message scope status = %d", response.Code)
	}
}

func TestMessageBoundaryPreservesScopeResponse(t *testing.T) {
	handlers := NewMessageHTTPHandlers(func(w http.ResponseWriter) (MessageScope, bool) {
		http.Error(w, "locked", http.StatusLocked)
		return MessageScope{}, false
	})
	response := httptest.NewRecorder()
	handlers.List(response, httptest.NewRequest(http.MethodGet, "/api/messages", nil))
	if response.Code != http.StatusLocked {
		t.Fatalf("scope response status = %d", response.Code)
	}
}

func performMessageRequest(t *testing.T, handler http.HandlerFunc, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func openMessageTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "messages.db"), "test-password")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertMessageTestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	stamp := time.Now().UnixNano()
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
		fmt.Sprintf("agent-%d", stamp), fmt.Sprintf("hash-%d", stamp), "aip_test")
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read token id: %v", err)
	}
	return id
}

func insertMessageTestRuntime(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, status, created_at, updated_at)
		VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped'), 'test', 'worker', '{}', 'active', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read target id: %v", err)
	}
	result, err = database.Exec(`
		INSERT INTO connector_credential_profiles
			(target_id, connector_kind, kind, label, public_json, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'default', '{}', '', 'active', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read profile id: %v", err)
	}
	result, err = database.Exec(`
		INSERT INTO connector_runtime_surfaces
			(connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, 'live_console', 'live_console', 'active', datetime('now'), datetime('now'))`, targetID, profileID)
	if err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	runtimeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read runtime id: %v", err)
	}
	return runtimeID
}
