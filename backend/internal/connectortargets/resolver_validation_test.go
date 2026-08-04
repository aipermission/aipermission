package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
)

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
	if _, err := database.Exec(`
		INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
		SELECT ?, id, 1, datetime('now'), datetime('now') FROM projects WHERE status = 'active'`, id); err != nil {
		t.Fatalf("insert token project scopes: %v", err)
	}
	return id
}
