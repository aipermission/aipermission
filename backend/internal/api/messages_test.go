package api

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestMessagesValidateListConsumeAndMarkRead(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()

	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, Direction: "bad", Message: "hello"}); err == nil {
		t.Fatalf("expected bad direction to fail")
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, Message: " "}); err == nil {
		t.Fatalf("expected empty message to fail")
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{Message: "hello"}); err == nil {
		t.Fatalf("expected missing token to fail")
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID + 999, Message: "hello"}); err == nil || !strings.Contains(err.Error(), "token_id") {
		t.Fatalf("expected unknown token to fail, got %v", err)
	}

	first, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &server.ID, Message: " first token=secret-value "})
	if err != nil {
		t.Fatalf("insert first message: %v", err)
	}
	if strings.Contains(first.Message, "secret-value") || !strings.Contains(first.Message, "[REDACTED]") {
		t.Fatalf("message should be redacted before storage and response: %q", first.Message)
	}
	otherServer := fixture.createKeyAndServer(t, "worker-2")
	missingRuntimeProfileID := otherServer.ID + 999
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &missingRuntimeProfileID, Message: "missing server"}); err == nil || !strings.Contains(err.Error(), "runtime_profile_id") {
		t.Fatalf("expected unknown server to fail, got %v", err)
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &otherServer.ID, Message: "other server"}); err != nil {
		t.Fatalf("insert other server message: %v", err)
	}
	second, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &server.ID, Direction: "ai_to_user", Message: "second"})
	if err != nil {
		t.Fatalf("insert second message: %v", err)
	}

	items, err := fixture.server.listMessageRecords(ctx, runtime, messageFilter{TokenID: token.ID})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("unexpected message count: %#v", items)
	}
	seen := map[int64]bool{items[0].ID: true, items[1].ID: true, items[2].ID: true}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("message list should include both messages: %#v", items)
	}

	note, err := fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, 0)
	if err != nil {
		t.Fatalf("consume message: %v", err)
	}
	if note == nil || strings.Contains(*note, "secret-value") || !strings.Contains(*note, "[REDACTED]") {
		t.Fatalf("unexpected consumed note: %#v", note)
	}
	note, err = fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, 0)
	if err != nil {
		t.Fatalf("consume empty message queue: %v", err)
	}
	if note != nil {
		t.Fatalf("expected note to be consumed once, got %#v", note)
	}
	note, err = fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, otherServer.ID, 0)
	if err != nil {
		t.Fatalf("consume other server message: %v", err)
	}
	if note == nil || *note != "other server" {
		t.Fatalf("expected other server note to stay scoped, got %#v", note)
	}

	response := performJSON(fixture.server.Handler(), "POST", "/api/messages/read", "", markMessagesReadRequest{RuntimeProfileID: server.ID})
	if response.Code != 200 {
		t.Fatalf("mark read failed: %d %s", response.Code, response.Body.String())
	}
	aiMessages, err := fixture.server.listMessageRecords(ctx, runtime, messageFilter{TokenID: token.ID, Direction: "ai_to_user"})
	if err != nil {
		t.Fatalf("list ai messages: %v", err)
	}
	if len(aiMessages) != 1 || aiMessages[0].ConsumedAt == nil {
		t.Fatalf("ai message should be marked read: %#v", aiMessages)
	}
}

func TestMessagesPreferMatchingSessionScope(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()

	sessionOne := insertMessageTestSession(t, fixture.db, server.ID, "session-one")
	sessionTwo := insertMessageTestSession(t, fixture.db, server.ID, "session-two")
	otherServer := fixture.createKeyAndServer(t, "worker-2")
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &otherServer.ID, SessionID: &sessionOne, Message: "wrong server"}); err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("expected mismatched session/server to fail, got %v", err)
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &server.ID, SessionID: &sessionOne, Message: "session one"}); err != nil {
		t.Fatalf("insert session one message: %v", err)
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &server.ID, Message: "server scoped"}); err != nil {
		t.Fatalf("insert server scoped message: %v", err)
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, Message: "generic"}); err != nil {
		t.Fatalf("insert generic message: %v", err)
	}
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, RuntimeProfileID: &server.ID, SessionID: &sessionTwo, Message: "session two"}); err != nil {
		t.Fatalf("insert session two message: %v", err)
	}

	note, err := fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, sessionTwo)
	if err != nil {
		t.Fatalf("consume session two message: %v", err)
	}
	if note == nil || *note != "session two" {
		t.Fatalf("expected session two note first, got %#v", note)
	}
	note, err = fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, 0)
	if err != nil {
		t.Fatalf("consume server scoped message: %v", err)
	}
	if note == nil || *note != "server scoped" {
		t.Fatalf("expected non-session read to skip session notes, got %#v", note)
	}
	note, err = fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, sessionOne)
	if err != nil {
		t.Fatalf("consume session one message: %v", err)
	}
	if note == nil || *note != "session one" {
		t.Fatalf("expected session one note, got %#v", note)
	}
	note, err = fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, 0)
	if err != nil {
		t.Fatalf("consume generic message: %v", err)
	}
	if note == nil || *note != "generic" {
		t.Fatalf("expected generic note last, got %#v", note)
	}
}

func insertMessageTestSession(t *testing.T, database interface {
	Exec(query string, args ...any) (sql.Result, error)
}, runtimeProfileID int64, name string) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO console_sessions (runtime_profile_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, ?, 'connected', '', 120, 32, datetime('now'), datetime('now'))`,
		runtimeProfileID,
		name,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read console session id: %v", err)
	}
	return id
}
