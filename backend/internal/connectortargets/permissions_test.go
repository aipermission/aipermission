package connectortargets

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

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

	inputs := []SetActionPermissionInput{
		{
			TargetID:      target.ID,
			ProfileID:     profile.ID,
			ActionName:    "query_readonly",
			ExecutionRule: ActionPermissionApprovalRequired,
			ExpiresAt:     &expiresAt,
		},
	}
	permissions, changed, err := store.ReplaceActionPermissionsWithChange(ctx, tokenID, inputs)
	if err != nil {
		t.Fatalf("replace action permissions: %v", err)
	}
	if !changed {
		t.Fatal("initial action permission replace was reported unchanged")
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

	permissions, changed, err = store.ReplaceActionPermissionsWithChange(ctx, tokenID, inputs)
	if err != nil || changed {
		t.Fatalf("identical action permission replace: changed=%v err=%v", changed, err)
	}

	inputs[0].ExpiresAt = nil
	permissions, changed, err = store.ReplaceActionPermissionsWithChange(ctx, tokenID, inputs)
	if err != nil || !changed {
		t.Fatalf("remove action permission expiry: changed=%v err=%v", changed, err)
	}
	permissions, changed, err = store.ReplaceActionPermissionsWithChange(ctx, tokenID, inputs)
	if err != nil || changed {
		t.Fatalf("identical permission without expiry: changed=%v err=%v", changed, err)
	}

	permissions, err = store.ReplaceActionPermissions(ctx, tokenID, nil)
	if err != nil {
		t.Fatalf("clear action permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("expected permissions to be cleared, got %#v", permissions)
	}
}

func TestStoreRejectsMismatchedTargetProfileKind(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	tokenID := insertConnectorTestToken(t, database)
	target, _ := createPostgresTargetProfile(t, ctx, store)
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json,
			status, created_at, updated_at
		)
		VALUES (?, 'ssh', 'private_key', 'wrong-kind', '{}', 'encrypted', 'active', ?, ?)`,
		target.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert mismatched profile: %v", err)
	}
	mismatchedProfileID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}

	if _, err := store.GetCredentialProfile(ctx, target.ID, mismatchedProfileID); !errors.Is(err, ErrTargetProfileNotFound) {
		t.Fatalf("mismatched profile should be hidden, got %v", err)
	}
	if err := store.SetActionPermission(ctx, SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     mismatchedProfileID,
		ActionName:    "query_readonly",
		ExecutionRule: ActionPermissionAlwaysRun,
	}); !errors.Is(err, ErrTargetProfileNotFound) {
		t.Fatalf("mismatched profile should be rejected for permissions, got %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, created_at, updated_at
		)
		VALUES (?, ?, ?, 'query_readonly', 'always_run', ?, ?)`,
		tokenID,
		target.ID,
		mismatchedProfileID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert mismatched permission: %v", err)
	}
	permissions, err := store.ListActionPermissions(ctx, tokenID)
	if err != nil {
		t.Fatalf("list permissions: %v", err)
	}
	if len(permissions) != 0 {
		t.Fatalf("mismatched permission should be hidden, got %#v", permissions)
	}
}

func TestStoreCredentialKindChangeRequiresSecret(t *testing.T) {
	database := openTargetTestDB(t)
	store := NewStore(database)
	ctx := context.Background()
	target, profile := createPostgresTargetProfile(t, ctx, store)

	if _, err := store.UpdateCredentialProfile(ctx, UpdateCredentialProfileInput{
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ConnectorKind: "postgres",
		Kind:          "token",
		Label:         "readonly",
		Public:        map[string]any{"username": "app_readonly"},
	}); err == nil {
		t.Fatal("expected credential kind change without secret to fail")
	}
	encrypted := "encrypted-token"
	updated, err := store.UpdateCredentialProfile(ctx, UpdateCredentialProfileInput{
		TargetID:            target.ID,
		ProfileID:           profile.ID,
		ConnectorKind:       "postgres",
		Kind:                "token",
		Label:               "token-profile",
		Public:              map[string]any{"username": "app_token"},
		EncryptedSecretJSON: &encrypted,
	})
	if err != nil {
		t.Fatalf("credential kind change with secret should succeed: %v", err)
	}
	if updated.Kind != "token" || updated.EncryptedSecretJSON != encrypted {
		t.Fatalf("unexpected updated profile: %#v", updated)
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
