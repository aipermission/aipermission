package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
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
