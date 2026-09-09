package messagequeue

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreValidatesScopesRedactsAndMarksRead(t *testing.T) {
	database := openTestDatabase(t)
	tokenID := insertTestToken(t, database)
	firstRuntime := insertTestRuntime(t, database, "first", "active", "live_console")
	secondRuntime := insertTestRuntime(t, database, "second", "active", "live_console")
	archivedRuntime := insertTestRuntime(t, database, "archived", "archived", "live_console")
	nonConsoleRuntime := insertTestRuntime(t, database, "action", "active", "structured_action")
	store := NewStore(database, func(_ context.Context, value string) string {
		return strings.ReplaceAll(value, "secret-value", "[REDACTED]")
	})

	for name, request := range map[string]CreateRequest{
		"missing token":    {Message: "hello"},
		"unknown token":    {TokenID: tokenID + 999, Message: "hello"},
		"empty message":    {TokenID: tokenID, Message: " "},
		"bad direction":    {TokenID: tokenID, Direction: "sideways", Message: "hello"},
		"archived runtime": {TokenID: tokenID, RuntimeID: &archivedRuntime, Message: "hello"},
		"wrong capability": {TokenID: tokenID, RuntimeID: &nonConsoleRuntime, Message: "hello"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Insert(context.Background(), request); err == nil {
				t.Fatal("expected insert to fail")
			}
		})
	}
	tooLarge := strings.Repeat("x", MaxMessageBytes+1)
	if _, err := store.Insert(context.Background(), CreateRequest{TokenID: tokenID, Message: tooLarge}); err == nil || !strings.Contains(err.Error(), "8192 bytes or less") {
		t.Fatalf("unexpected size validation: %v", err)
	}

	first, err := store.Insert(context.Background(), CreateRequest{
		TokenID: tokenID, RuntimeID: &firstRuntime, Message: " token=secret-value ",
	})
	if err != nil {
		t.Fatalf("insert first message: %v", err)
	}
	if first.Message != "token=[REDACTED]" || first.TargetName != "first" {
		t.Fatalf("unexpected persisted record: %#v", first)
	}
	if _, err := store.Insert(context.Background(), CreateRequest{
		TokenID: tokenID, RuntimeID: &secondRuntime, Direction: "ai_to_user", Message: "reply",
	}); err != nil {
		t.Fatalf("insert reply: %v", err)
	}
	items, err := store.List(context.Background(), Filter{RuntimeID: firstRuntime})
	if err != nil || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("filtered list mismatch: items=%#v err=%v", items, err)
	}
	count, err := store.MarkRuntimeRead(context.Background(), secondRuntime)
	if err != nil || count != 1 {
		t.Fatalf("mark read: count=%d err=%v", count, err)
	}
	replies, err := store.List(context.Background(), Filter{Direction: "ai_to_user"})
	if err != nil || len(replies) != 1 || replies[0].ConsumedAt == nil {
		t.Fatalf("marked reply mismatch: items=%#v err=%v", replies, err)
	}
}

func TestStorePrefersSessionThenRuntimeThenGeneric(t *testing.T) {
	database := openTestDatabase(t)
	tokenID := insertTestToken(t, database)
	runtimeID := insertTestRuntime(t, database, "worker", "active", "live_console")
	otherRuntimeID := insertTestRuntime(t, database, "other", "active", "live_console")
	sessionOne := insertTestSession(t, database, runtimeID, "one")
	sessionTwo := insertTestSession(t, database, runtimeID, "two")
	store := NewStore(database, nil)

	mismatch := CreateRequest{TokenID: tokenID, RuntimeID: &otherRuntimeID, SessionID: &sessionOne, Message: "wrong"}
	if _, err := store.Insert(context.Background(), mismatch); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("expected session mismatch, got %v", err)
	}
	for _, request := range []CreateRequest{
		{TokenID: tokenID, RuntimeID: &runtimeID, SessionID: &sessionOne, Message: "session-one"},
		{TokenID: tokenID, RuntimeID: &runtimeID, Message: "runtime"},
		{TokenID: tokenID, Message: "generic"},
		{TokenID: tokenID, RuntimeID: &runtimeID, SessionID: &sessionTwo, Message: "session-two"},
	} {
		if _, err := store.Insert(context.Background(), request); err != nil {
			t.Fatalf("insert %q: %v", request.Message, err)
		}
	}
	assertNextMessage(t, store, tokenID, runtimeID, sessionTwo, "session-two")
	assertNextMessage(t, store, tokenID, runtimeID, 0, "runtime")
	assertNextMessage(t, store, tokenID, runtimeID, sessionOne, "session-one")
	assertNextMessage(t, store, tokenID, runtimeID, 0, "generic")
	if note, err := store.ConsumeNextUser(context.Background(), tokenID, runtimeID, 0); err != nil || note != nil {
		t.Fatalf("expected empty queue, note=%v err=%v", note, err)
	}
}

func TestStoreConsumesMessageAtMostOnceConcurrently(t *testing.T) {
	database := openTestDatabase(t)
	tokenID := insertTestToken(t, database)
	runtimeID := insertTestRuntime(t, database, "worker", "active", "live_console")
	store := NewStore(database, nil)
	if _, err := store.Insert(context.Background(), CreateRequest{TokenID: tokenID, RuntimeID: &runtimeID, Message: "once"}); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	var consumed atomic.Int32
	var failures atomic.Int32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			note, err := store.ConsumeNextUser(context.Background(), tokenID, runtimeID, 0)
			if err != nil {
				failures.Add(1)
				return
			}
			if note != nil {
				consumed.Add(1)
			}
		}()
	}
	wait.Wait()
	if failures.Load() != 0 || consumed.Load() != 1 {
		t.Fatalf("concurrent consume failures=%d consumed=%d", failures.Load(), consumed.Load())
	}
}

func TestEnqueueUserNoteParticipatesInCallerTransaction(t *testing.T) {
	database := openTestDatabase(t)
	tokenID := insertTestToken(t, database)
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if err := EnqueueUserNote(context.Background(), tx, tokenID, " operator note "); err != nil {
		t.Fatalf("enqueue note: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	items, err := NewStore(database, nil).List(context.Background(), Filter{TokenID: tokenID})
	if err != nil || len(items) != 0 {
		t.Fatalf("rolled-back note persisted: items=%#v err=%v", items, err)
	}

	if err := EnqueueUserNote(context.Background(), database, tokenID, " operator note "); err != nil {
		t.Fatalf("enqueue committed note: %v", err)
	}
	items, err = NewStore(database, nil).List(context.Background(), Filter{TokenID: tokenID})
	if err != nil || len(items) != 1 || items[0].Message != "operator note" {
		t.Fatalf("committed note mismatch: items=%#v err=%v", items, err)
	}
}

func assertNextMessage(t *testing.T, store *Store, tokenID, runtimeID, sessionID int64, want string) {
	t.Helper()
	peeked, err := store.NextUser(context.Background(), tokenID, runtimeID, sessionID)
	if err != nil || peeked.Message != want {
		t.Fatalf("peek next: got=%#v want=%q err=%v", peeked, want, err)
	}
	note, err := store.ConsumeNextUser(context.Background(), tokenID, runtimeID, sessionID)
	if err != nil || note == nil || *note != want {
		t.Fatalf("consume next: got=%v want=%q err=%v", note, want, err)
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "messages.db"), "test-password")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertTestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
		fmt.Sprintf("agent-%d", time.Now().UnixNano()), fmt.Sprintf("hash-%d", time.Now().UnixNano()), "aip_test")
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}

func insertTestRuntime(t *testing.T, database *sql.DB, name, status, capability string) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, status, created_at, updated_at)
		VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped'), 'test', ?, '{}', 'active', datetime('now'), datetime('now'))`, name)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	targetID, _ := result.LastInsertId()
	result, err = database.Exec(`
		INSERT INTO connector_credential_profiles
			(target_id, connector_kind, kind, label, public_json, encrypted_secret_json, status, created_at, updated_at)
		VALUES (?, 'test', 'test', 'default', '{}', '', 'active', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	profileID, _ := result.LastInsertId()
	result, err = database.Exec(`
		INSERT INTO connector_runtime_surfaces
			(connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at)
		VALUES ('test', ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`, targetID, profileID, capability, capability, status)
	if err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	runtimeID, _ := result.LastInsertId()
	return runtimeID
}

func insertTestSession(t *testing.T, database *sql.DB, runtimeID int64, name string) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO console_sessions (runtime_id, generation, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, ?, ?, 'connected', '', 120, 32, datetime('now'), datetime('now'))`, runtimeID, time.Now().UnixNano(), name)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}
