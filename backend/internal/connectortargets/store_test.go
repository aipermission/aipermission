package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestConnectorTargetRefRoundTrip(t *testing.T) {
	ref := ConnectorTargetRef("postgres", 42, 7)
	if ref != "postgres:42:7" {
		t.Fatalf("ref = %q", ref)
	}
	kind, targetID, profileID, ok := ParseConnectorTargetRef(ref)
	if !ok || kind != "postgres" || targetID != 42 || profileID != 7 {
		t.Fatalf("parse = %q %d %d ok=%v", kind, targetID, profileID, ok)
	}
}

func TestParseConnectorTargetRefRejectsInvalidRefs(t *testing.T) {
	for _, ref := range []string{"", "ssh:1", "postgres", "Postgres:1:2", "postgres:0:2", "postgres:1:0", "postgres:x:2"} {
		if _, _, _, ok := ParseConnectorTargetRef(ref); ok {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}

func TestStoreCreatesAndResolvesConnectorTargetProfile(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()

	target, profile := createPostgresTargetProfile(t, ctx, store)
	targets, err := store.ListTargets(ctx, ListTargetsFilter{ConnectorKind: "postgres"})
	if err != nil {
		t.Fatalf("list connector targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != target.ID || targets[0].Config["database"] != "app" {
		t.Fatalf("unexpected target list: %#v", targets)
	}
	gotTarget, err := store.GetTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("get connector target: %v", err)
	}
	if gotTarget.Name != "main-db" || gotTarget.ConnectorKind != "postgres" {
		t.Fatalf("unexpected target: %#v", gotTarget)
	}
	profiles, err := store.ListCredentialProfiles(ctx, target.ID)
	if err != nil {
		t.Fatalf("list connector profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != profile.ID || profiles[0].EncryptedSecretJSON != "encrypted-secret" {
		t.Fatalf("unexpected profile list: %#v", profiles)
	}
	gotProfile, err := store.GetCredentialProfile(ctx, target.ID, profile.ID)
	if err != nil {
		t.Fatalf("get connector profile: %v", err)
	}
	if gotProfile.EncryptedSecretJSON != "encrypted-secret" || gotProfile.Public["username"] != "app_readonly" {
		t.Fatalf("unexpected profile: %#v", gotProfile)
	}

	resolvedTarget, resolvedProfile, err := store.ResolveConnectorActionTarget(ctx, ConnectorTargetRef("postgres", target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve connector target: %v", err)
	}

	if resolvedTarget.Ref != ConnectorTargetRef("postgres", target.ID, profile.ID) {
		t.Fatalf("target ref = %q", resolvedTarget.Ref)
	}
	if resolvedTarget.ConnectorKind != "postgres" || resolvedTarget.Name != "main-db" {
		t.Fatalf("unexpected target: %#v", resolvedTarget)
	}
	if resolvedTarget.Config["host"] != "10.0.0.15" || resolvedTarget.Config["port"].(float64) != 5432 {
		t.Fatalf("unexpected target config: %#v", resolvedTarget.Config)
	}
	if resolvedProfile.ID != profile.ID || resolvedProfile.TargetID != target.ID {
		t.Fatalf("unexpected profile identity: %#v", resolvedProfile)
	}
	if resolvedProfile.Public["username"] != "app_readonly" {
		t.Fatalf("profile public metadata missing: %#v", resolvedProfile.Public)
	}
	if _, exists := resolvedProfile.Public["password"]; exists {
		t.Fatalf("secret should not be exposed in public metadata: %#v", resolvedProfile.Public)
	}
}

func TestStoreGetTargetReturnsNotFound(t *testing.T) {
	_, err := NewStore(openTargetTestDB(t)).GetTarget(context.Background(), 999)
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestStoreSetActionPermissionUpsertsRuleAndExpiration(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	expiresAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionApprovalRequired,
		ExpiresAt:     &expiresAt,
	}); err != nil {
		t.Fatalf("set connector action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("upsert connector action permission: %v", err)
	}

	var rule string
	var expires sql.NullString
	if err := database.QueryRow(`
		SELECT execution_rule, expires_at
		FROM token_connector_action_permissions
		WHERE token_id = ? AND target_id = ? AND profile_id = ? AND action_name = 'query_readonly'`,
		tokenID,
		target.ID,
		profile.ID,
	).Scan(&rule, &expires); err != nil {
		t.Fatalf("read connector action permission: %v", err)
	}
	var count int
	if err := database.QueryRow(`
		SELECT COUNT(*)
		FROM token_connector_action_permissions
		WHERE token_id = ? AND target_id = ? AND profile_id = ? AND action_name = 'query_readonly'`,
		tokenID,
		target.ID,
		profile.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count connector action permissions: %v", err)
	}
	if count != 1 || rule != string(ActionPermissionAlwaysRun) {
		t.Fatalf("unexpected permission count/rule: count=%d rule=%q", count, rule)
	}
	if expires.Valid {
		t.Fatalf("expires_at should be cleared by second upsert, got %q", expires.String)
	}
}

func TestStoreReplaceAndListActionPermissions(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)
	expiresAt := time.Now().UTC().Add(time.Hour)

	permissions, err := store.ReplaceActionPermissions(ctx, tokenID, []SetActionPermissionInput{
		{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ActionName:    "query_readonly",
			ExecutionRule: ActionPermissionApprovalRequired,
			ExpiresAt:     &expiresAt,
		},
	})
	if err != nil {
		t.Fatalf("replace action permissions: %v", err)
	}
	if len(permissions) != 1 {
		t.Fatalf("expected 1 permission, got %#v", permissions)
	}
	got := permissions[0]
	if got.TokenID != tokenID || got.TargetName != "main-db" || got.ProfileLabel != "readonly" || got.ConnectorKind != "postgres" {
		t.Fatalf("unexpected permission metadata: %#v", got)
	}
	if got.ExecutionRule != ActionPermissionApprovalRequired || got.ExpiresAt == "" {
		t.Fatalf("unexpected permission rule/expiry: %#v", got)
	}

	permissions, err = store.ReplaceActionPermissions(ctx, tokenID, nil)
	if err != nil {
		t.Fatalf("clear action permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("expected permissions to be cleared, got %#v", permissions)
	}
}

func TestStoreGetsActiveActionPermission(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	expiresAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionApprovalRequired,
		ExpiresAt:     &expiresAt,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	permission, err := store.GetActionPermission(ctx, tokenID, target.ID, profile.ID, "query_readonly", expiresAt.Add(-time.Minute))
	if err != nil {
		t.Fatalf("get active action permission: %v", err)
	}
	if permission.ExecutionRule != ActionPermissionApprovalRequired || permission.TargetName != "main-db" {
		t.Fatalf("unexpected permission: %#v", permission)
	}

	_, err = store.GetActionPermission(ctx, tokenID, target.ID, profile.ID, "query_readonly", expiresAt.Add(time.Minute))
	if !errors.Is(err, ErrActionPermissionNotFound) {
		t.Fatalf("expected expired permission to be hidden, got %v", err)
	}
}

func TestStoreListActionPermissionsHidesExpiredPermissions(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, profile := createPostgresTargetProfile(t, ctx, store)

	expiredAt := time.Now().UTC().Add(-time.Minute)
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionAlwaysRun,
		ExpiresAt:     &expiredAt,
	}); err != nil {
		t.Fatalf("set expired action permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    "get_tables",
		ExecutionRule: ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set active action permission: %v", err)
	}

	permissions, err := store.ListActionPermissions(ctx, tokenID)
	if err != nil {
		t.Fatalf("list action permissions: %v", err)
	}
	if len(permissions) != 1 || permissions[0].ActionName != "get_tables" {
		t.Fatalf("expected only active permissions, got %#v", permissions)
	}
}

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
		Input:                map[string]any{"sql": "select 1", "max_rows": 10},
		EncryptedPayloadJSON: "encrypted-payload",
		Reason:               "smoke",
		Status:               connectors.ResultRunning,
		ApprovalContext:      `{"target":"postgres:1:1"}`,
		ApprovalContextHash:  "ctx-hash",
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

func TestResolverMapsGenericConnectorRefToConnectorViews(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	target, profile := createPostgresTargetProfile(t, context.Background(), store)

	resolved, err := NewResolver(database).ResolveActionTarget(context.Background(), ConnectorTargetRef("postgres", target.ID, profile.ID))
	if err != nil {
		t.Fatalf("resolve generic connector target: %v", err)
	}
	if resolved.Target.ConnectorKind != "postgres" || resolved.Profile.Label != "readonly" {
		t.Fatalf("unexpected resolved target/profile: %#v", resolved)
	}

	_, err = NewResolver(database).ResolveActionTarget(context.Background(), ConnectorTargetRef("postgres", target.ID, profile.ID+100))
	if !errors.Is(err, actions.ErrTargetNotFound) {
		t.Fatalf("missing generic profile error = %v", err)
	}
}

func TestStoreRejectsInvalidConnectorInputs(t *testing.T) {
	store := NewStore(openTargetTestDB(t))
	if _, err := store.CreateTarget(context.Background(), CreateTargetInput{ConnectorKind: "Bad", Name: "bad"}); err == nil {
		t.Fatal("expected invalid connector kind error")
	}
	if _, err := store.CreateCredentialProfile(context.Background(), CreateCredentialProfileInput{
		TargetID:      1,
		ConnectorKind: "postgres",
		Kind:          "bad-kind",
		Label:         "bad",
	}); err == nil {
		t.Fatal("expected invalid credential kind error")
	}
	target, err := store.CreateTarget(context.Background(), CreateTargetInput{
		ConnectorKind: "postgres",
		Name:          "main-db",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if _, err := store.CreateCredentialProfile(context.Background(), CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: "redis",
		Kind:          "username_password",
		Label:         "wrong-kind",
	}); !errors.Is(err, ErrTargetProfileNotFound) {
		t.Fatalf("expected target/profile not found for connector mismatch, got %v", err)
	}
	if err := store.SetActionPermission(context.Background(), SetActionPermissionInput{
		TokenID:       1,
		TargetID:      1,
		ProfileID:     1,
		ActionName:    "bad-action",
		ExecutionRule: ActionPermissionAlwaysRun,
	}); err == nil {
		t.Fatal("expected invalid action name error")
	}
}

func createPostgresTargetProfile(t *testing.T, ctx context.Context, store *Store) (Target, CredentialProfile) {
	t.Helper()
	target, err := store.CreateTarget(ctx, CreateTargetInput{
		ConnectorKind: "postgres",
		Name:          "main-db",
		Config: map[string]any{
			"mode":     "direct",
			"host":     "10.0.0.15",
			"port":     5432,
			"database": "app",
		},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       "postgres",
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly"},
		EncryptedSecretJSON: "encrypted-secret",
		RiskLabel:           "low",
	})
	if err != nil {
		t.Fatalf("create credential profile: %v", err)
	}
	return target, profile
}

func insertConnectorTestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex', 'connector-hash', 'aip_conn', datetime('now'), datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	return id
}
