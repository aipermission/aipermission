package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestStoreActionRequestLifecycle(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "query_readonly",
		Title:                "Run Postgres read-only query",
		Summary:              "Run a bounded read-only SQL query",
		Preview:              map[string]any{"sql": "select 1", "max_rows": 10},
		Input:                map[string]any{"sql": "select 1", "max_rows": 10},
		EncryptedPayloadJSON: "encrypted-payload",
		Reason:               "smoke",
		Status:               connectors.ResultRunning,
		ApprovalContext:      `{"target":"postgres:1:1"}`,
		ApprovalContextHash:  "ctx-hash",
		RetryPolicy: connectors.RetryPolicy{
			Class: connectors.RetryReadOnly, Guidance: "inspect before repeating",
		},
	})
	if err != nil {
		t.Fatalf("insert action request: %v", err)
	}
	if request.ID < 1 || request.TokenID == nil || *request.TokenID != tokenID {
		t.Fatalf("unexpected request identity: %#v", request)
	}
	if request.Source != "mcp" {
		t.Fatalf("default source = %q", request.Source)
	}
	if request.TokenName != "connector-codex" {
		t.Fatalf("token name = %q", request.TokenName)
	}
	if request.TargetName != "main-db" || request.ProfileLabel != "readonly" || request.ConnectorKind != "postgres" {
		t.Fatalf("unexpected request metadata: %#v", request)
	}
	if request.Input["sql"] != "select 1" || request.EncryptedPayloadJSON != "encrypted-payload" {
		t.Fatalf("unexpected request payload fields: %#v", request)
	}
	if request.Title != "Run Postgres read-only query" || request.Summary != "Run a bounded read-only SQL query" || request.Preview["sql"] != "select 1" {
		t.Fatalf("unexpected request display metadata: %#v", request)
	}
	if request.RetryPolicy.Class != connectors.RetryReadOnly || request.RetryPolicy.Guidance != "inspect before repeating" {
		t.Fatalf("unexpected request retry policy: %#v", request.RetryPolicy)
	}
	var historyRetryPolicy string
	if err := database.QueryRow(`SELECT retry_policy_json FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, request.ID).Scan(&historyRetryPolicy); err != nil {
		t.Fatalf("read history retry policy: %v", err)
	}
	if !strings.Contains(historyRetryPolicy, `"class":"read_only"`) {
		t.Fatalf("history retry policy = %s", historyRetryPolicy)
	}
	if output, ok := request.Output.(map[string]any); !ok || len(output) != 0 {
		t.Fatalf("new request output should be empty object, got %#v", request.Output)
	}

	finished, err := store.FinishActionRequest(ctx, FinishActionRequestInput{
		ID:          request.ID,
		Status:      connectors.ResultCompleted,
		Output:      []map[string]any{{"one": 1}},
		DisplayText: "1 row",
	})
	if err != nil {
		t.Fatalf("finish action request: %v", err)
	}
	if finished.Status != connectors.ResultCompleted || finished.CompletedAt == nil {
		t.Fatalf("unexpected finished request: %#v", finished)
	}
	rows, ok := finished.Output.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("unexpected output shape: %#v", finished.Output)
	}
	if finished.DisplayText != "1 row" {
		t.Fatalf("display text = %q", finished.DisplayText)
	}
}

func TestStoreActionRequestIdempotency(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := t.Context()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)
	input := InsertActionRequestInput{
		TokenID: &tokenID, TargetID: target.ID, ProfileID: profile.ID,
		ConnectorKind: "postgres", ActionName: "query_readonly",
		Input: map[string]any{"sql": "select 1"}, Status: connectors.ResultApprovalPending,
		IdempotencyKey: "stable-request", IdempotencyIdentityHash: "identity-one",
	}
	first, created, err := store.InsertActionRequestIdempotent(ctx, input)
	if err != nil || !created {
		t.Fatalf("create idempotent request: created=%v err=%v", created, err)
	}
	second, created, err := store.InsertActionRequestIdempotent(ctx, input)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay request: first=%d second=%d created=%v err=%v", first.ID, second.ID, created, err)
	}
	input.IdempotencyIdentityHash = "identity-two"
	if _, _, err := store.InsertActionRequestIdempotent(ctx, input); !errors.Is(err, ErrActionRequestIdempotency) {
		t.Fatalf("conflicting identity error = %v", err)
	}
	var requestCount, historyCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connector_action_requests WHERE idempotency_key = ?`, input.IdempotencyKey).Scan(&requestCount); err != nil {
		t.Fatalf("count requests: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, first.ID).Scan(&historyCount); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if requestCount != 1 || historyCount != 1 {
		t.Fatalf("idempotent projection counts request=%d history=%d", requestCount, historyCount)
	}
	secondToken, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex-two', 'connector-hash-two', 'aip_two', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert second token: %v", err)
	}
	secondTokenID, err := secondToken.LastInsertId()
	if err != nil {
		t.Fatalf("second token id: %v", err)
	}
	input.TokenID = &secondTokenID
	input.IdempotencyIdentityHash = "identity-for-token-two"
	if _, created, err := store.InsertActionRequestIdempotent(ctx, input); err != nil || !created {
		t.Fatalf("same caller key in a second token scope: created=%v err=%v", created, err)
	}
	if _, err := database.Exec(`DELETE FROM api_tokens WHERE id IN (?, ?)`, tokenID, secondTokenID); err != nil {
		t.Fatalf("delete tokens while retaining idempotent history: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM connector_action_requests WHERE token_id IS NULL AND idempotency_key = ?`, input.IdempotencyKey).Scan(&requestCount); err != nil {
		t.Fatalf("count retained requests: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("retained idempotent requests=%d", requestCount)
	}
}

func TestStoreActionRequestHistoryProjectionIsAtomic(t *testing.T) {
	t.Run("insert", func(t *testing.T) {
		database := openTargetTestDB(t)
		store := NewStore(database)
		ctx := t.Context()
		target, profile := createPostgresTargetProfile(t, ctx, store)
		if _, err := database.Exec(`DROP TABLE history_entries`); err != nil {
			t.Fatalf("drop history projection: %v", err)
		}

		_, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
			TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "postgres",
			ActionName: "query_readonly", Status: connectors.ResultRunning,
		})
		if err == nil {
			t.Fatalf("insert should fail when its history projection cannot be written")
		}
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM connector_action_requests`).Scan(&count); err != nil {
			t.Fatalf("count canonical requests: %v", err)
		}
		if count != 0 {
			t.Fatalf("failed history projection should roll back canonical insert, count=%d", count)
		}
	})

	t.Run("claim", func(t *testing.T) {
		database := openTargetTestDB(t)
		store := NewStore(database)
		ctx := t.Context()
		target, profile := createPostgresTargetProfile(t, ctx, store)
		request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
			TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "postgres",
			ActionName: "query_readonly", Status: connectors.ResultApprovalPending,
		})
		if err != nil {
			t.Fatalf("insert pending request: %v", err)
		}
		if _, err := database.Exec(`DROP TABLE history_entries`); err != nil {
			t.Fatalf("drop history projection: %v", err)
		}

		if _, err := store.MarkActionRequestRunning(ctx, request.ID); err == nil {
			t.Fatalf("claim should fail when its history projection cannot be written")
		}
		var status string
		if err := database.QueryRow(`SELECT status FROM connector_action_requests WHERE id = ?`, request.ID).Scan(&status); err != nil {
			t.Fatalf("read canonical request: %v", err)
		}
		if status != string(connectors.ResultApprovalPending) {
			t.Fatalf("failed history projection should roll back claim, status=%s", status)
		}
	})

	t.Run("finish", func(t *testing.T) {
		database := openTargetTestDB(t)
		store := NewStore(database)
		ctx := t.Context()
		target, profile := createPostgresTargetProfile(t, ctx, store)
		request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
			TargetID: target.ID, ProfileID: profile.ID, ConnectorKind: "postgres",
			ActionName: "query_readonly", Status: connectors.ResultRunning,
		})
		if err != nil {
			t.Fatalf("insert running request: %v", err)
		}
		if _, err := database.Exec(`DROP TABLE history_entries`); err != nil {
			t.Fatalf("drop history projection: %v", err)
		}

		if _, err := store.FinishActionRequest(ctx, FinishActionRequestInput{
			ID: request.ID, Status: connectors.ResultCompleted, Output: map[string]any{"ok": true},
		}); err == nil {
			t.Fatalf("finish should fail when its history projection cannot be written")
		}
		var status string
		if err := database.QueryRow(`SELECT status FROM connector_action_requests WHERE id = ?`, request.ID).Scan(&status); err != nil {
			t.Fatalf("read canonical request: %v", err)
		}
		if status != string(connectors.ResultRunning) {
			t.Fatalf("failed history projection should roll back terminal state, status=%s", status)
		}
	})
}

func TestStoreFinishActionRequestDoesNotOverwriteInvalidatedRequest(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "query_readonly",
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running request: %v", err)
	}
	if _, err := store.InvalidateActionRequestsForTarget(ctx, InvalidateActionRequestsForTargetInput{
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		Error:          "target changed",
		RunningError:   "target changed after dispatch",
		IncludeRunning: true,
	}); err != nil {
		t.Fatalf("invalidate request: %v", err)
	}

	finished, err := store.FinishActionRequest(ctx, FinishActionRequestInput{
		ID:          request.ID,
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"ok": true},
		DisplayText: "late success",
	})
	if err != nil {
		t.Fatalf("late finish should return current request without failing: %v", err)
	}
	if finished.Status != connectors.ResultOutcomeUnknown || finished.Error != "target changed after dispatch" || finished.DisplayText != "" {
		t.Fatalf("late finish overwrote invalidated request: %#v", finished)
	}
}

func TestStoreDeleteTargetArchivesAndPreservesActionRequests(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "query_readonly",
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultCompleted,
	})
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}

	if err := store.DeleteTarget(ctx, target.ID); err != nil {
		t.Fatalf("archive target: %v", err)
	}
	if _, err := store.GetTarget(ctx, target.ID); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("archived target should be hidden, got %v", err)
	}
	permissions, err := store.ListActionPermissions(ctx, tokenID)
	if err != nil {
		t.Fatalf("list permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("permissions for archived target should be hidden, got %#v", permissions)
	}
	assertPermissionRows(t, database, target.ID, profile.ID, 0)
	got, err := store.GetActionRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("history request should remain readable: %v", err)
	}
	if got.TargetName != "main-db" || got.ProfileLabel != "readonly" || got.Status != connectors.ResultCompleted {
		t.Fatalf("archived metadata was not preserved: %#v", got)
	}
	if _, err := store.CreateTarget(ctx, CreateTargetInput{
		ConnectorKind: "postgres",
		Name:          "main-db",
		Config:        map[string]any{"host": "127.0.0.1", "port": 5432, "database": "app"},
	}); err != nil {
		t.Fatalf("active target name should be reusable after archive: %v", err)
	}
}

func TestStoreDeleteCredentialProfileArchivesAndPreservesActionRequests(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "query_readonly",
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultCompleted,
	})
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}

	if err := store.DeleteCredentialProfile(ctx, target.ID, profile.ID); err != nil {
		t.Fatalf("archive profile: %v", err)
	}
	profiles, err := store.ListCredentialProfiles(ctx, target.ID)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("archived profile should be hidden, got %#v", profiles)
	}
	assertPermissionRows(t, database, target.ID, profile.ID, 0)
	got, err := store.GetActionRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("history request should remain readable: %v", err)
	}
	if got.ProfileLabel != "readonly" || got.TargetName != "main-db" {
		t.Fatalf("archived profile metadata was not preserved: %#v", got)
	}
	if _, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       "postgres",
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly_v2"},
		EncryptedSecretJSON: "encrypted-secret-v2",
	}); err != nil {
		t.Fatalf("active profile label should be reusable after archive: %v", err)
	}
}

func assertPermissionRows(t *testing.T, database *sql.DB, targetID int64, profileID int64, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM token_connector_action_permissions
		WHERE target_id = ? AND profile_id = ?`,
		targetID,
		profileID,
	).Scan(&count); err != nil {
		t.Fatalf("count permission rows: %v", err)
	}
	if count != want {
		t.Fatalf("permission row count = %d, want %d", count, want)
	}
}

func TestStoreActionRequestApprovalHelpers(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	request, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "query_readonly",
		Input:                map[string]any{"sql": "select 1"},
		EncryptedPayloadJSON: "encrypted",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending action request: %v", err)
	}
	pending, err := store.ListActionRequests(ctx, ActionRequestFilter{Status: string(connectors.ResultApprovalPending)})
	if err != nil {
		t.Fatalf("list pending action requests: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("unexpected pending requests: %#v", pending)
	}
	running, err := store.MarkActionRequestRunning(ctx, request.ID)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if running.Status != connectors.ResultRunning {
		t.Fatalf("status = %q", running.Status)
	}
	if _, err := store.DeclineActionRequest(ctx, request.ID, "no"); !errors.Is(err, ErrActionRequestNotPending) {
		t.Fatalf("expected running request not to decline, got %v", err)
	}

	second, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:              &tokenID,
		TargetID:             target.ID,
		ProfileID:            profile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "get_schemas",
		Input:                map[string]any{},
		EncryptedPayloadJSON: "encrypted",
		Status:               connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert second pending action request: %v", err)
	}
	declined, err := store.DeclineActionRequest(ctx, second.ID, "not this profile")
	if err != nil {
		t.Fatalf("decline action request: %v", err)
	}
	if declined.Status != connectors.ResultDeclined || declined.CompletedAt == nil || declined.Error != "not this profile" {
		t.Fatalf("unexpected declined request: %#v", declined)
	}
}

func TestStoreInvalidateActionRequestsForTargetSeparatesRunningOutcome(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	pending, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "get_tables",
		Input:         map[string]any{},
		Status:        connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending action request: %v", err)
	}
	running, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "query_readonly",
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running action request: %v", err)
	}
	completed, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "get_schemas",
		Input:         map[string]any{},
		Status:        connectors.ResultCompleted,
	})
	if err != nil {
		t.Fatalf("insert completed action request: %v", err)
	}

	result, err := store.InvalidateActionRequestsForTarget(ctx, InvalidateActionRequestsForTargetInput{
		TargetID:       target.ID,
		ProfileID:      profile.ID,
		Error:          "target deleted",
		RunningError:   "target deleted after dispatch; outcome unknown",
		ApprovalDrift:  "profile",
		IncludeRunning: true,
	})
	if err != nil {
		t.Fatalf("stale action requests: %v", err)
	}
	if result.Affected != 2 || len(result.IDs) != 2 || len(result.StaleIDs) != 1 || len(result.OutcomeUnknownIDs) != 1 {
		t.Fatalf("unexpected stale result: %#v", result)
	}
	gotPending, err := store.GetActionRequest(ctx, pending.ID)
	if err != nil {
		t.Fatalf("read stale request: %v", err)
	}
	if gotPending.Status != connectors.ResultStale || gotPending.Error != "target deleted" || gotPending.ApprovalContextDrift != "profile" || gotPending.CompletedAt == nil {
		t.Fatalf("pending request was not marked stale: %#v", gotPending)
	}
	gotRunning, err := store.GetActionRequest(ctx, running.ID)
	if err != nil {
		t.Fatalf("read outcome-unknown request: %v", err)
	}
	if gotRunning.Status != connectors.ResultOutcomeUnknown || gotRunning.Error != "target deleted after dispatch; outcome unknown" || gotRunning.ApprovalContextDrift != "profile" || gotRunning.CompletedAt == nil {
		t.Fatalf("running request was not marked outcome_unknown: %#v", gotRunning)
	}
	unchanged, err := store.GetActionRequest(ctx, completed.ID)
	if err != nil {
		t.Fatalf("read completed request: %v", err)
	}
	if unchanged.Status != connectors.ResultCompleted || unchanged.Error != "" || unchanged.CompletedAt != nil {
		t.Fatalf("completed request should remain unchanged: %#v", unchanged)
	}
}

func TestStoreInvalidateActionRequestsForTargetLeavesRunningByDefault(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	pending, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "get_tables",
		Input:         map[string]any{},
		Status:        connectors.ResultApprovalPending,
	})
	if err != nil {
		t.Fatalf("insert pending action request: %v", err)
	}
	running, err := store.InsertActionRequest(ctx, InsertActionRequestInput{
		TokenID:       &tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		ActionName:    "query_readonly",
		Input:         map[string]any{"sql": "select 1"},
		Status:        connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert running action request: %v", err)
	}

	result, err := store.InvalidateActionRequestsForTarget(ctx, InvalidateActionRequestsForTargetInput{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		Error:         "target updated",
		ApprovalDrift: "profile",
	})
	if err != nil {
		t.Fatalf("stale action requests: %v", err)
	}
	if result.Affected != 1 || len(result.IDs) != 1 || result.IDs[0] != pending.ID {
		t.Fatalf("unexpected stale result: %#v", result)
	}
	gotPending, err := store.GetActionRequest(ctx, pending.ID)
	if err != nil {
		t.Fatalf("read pending request: %v", err)
	}
	if gotPending.Status != connectors.ResultStale {
		t.Fatalf("pending request should be stale: %#v", gotPending)
	}
	gotRunning, err := store.GetActionRequest(ctx, running.ID)
	if err != nil {
		t.Fatalf("read running request: %v", err)
	}
	if gotRunning.Status != connectors.ResultRunning {
		t.Fatalf("running request should remain running by default: %#v", gotRunning)
	}
}

func TestStoreReplaceActionPermissionsValidatesInput(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)
	expiresAt := time.Now().UTC().Add(time.Hour)

	_, err := store.ReplaceActionPermissions(ctx, tokenID, []SetActionPermissionInput{
		{TargetID: target.ID, ProfileID: profile.ID, ActionName: "query_readonly", ExecutionRule: ActionPermissionAlwaysRun},
		{TargetID: target.ID, ProfileID: profile.ID, ActionName: "query_readonly", ExecutionRule: ActionPermissionApprovalRequired},
	})
	if err == nil {
		t.Fatal("expected duplicate permission validation error")
	}

	_, err = store.ReplaceActionPermissions(ctx, tokenID, []SetActionPermissionInput{
		{TargetID: target.ID, ProfileID: profile.ID, ActionName: "query_readonly", ExecutionRule: ActionPermissionBlocked, ExpiresAt: &expiresAt},
	})
	if err == nil {
		t.Fatal("expected blocked expires_at validation error")
	}

	_, err = store.ReplaceActionPermissions(ctx, tokenID, []SetActionPermissionInput{
		{TargetID: target.ID, ProfileID: profile.ID + 100, ActionName: "query_readonly", ExecutionRule: ActionPermissionAlwaysRun},
	})
	if err == nil {
		t.Fatal("expected missing profile validation error")
	}
}
