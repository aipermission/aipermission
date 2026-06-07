package tokens

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func openTokenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertTokenTestServer(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO servers (name, host, port, username, ssh_key_id, description, created_at, updated_at)
		VALUES (?, '127.0.0.1', 22, 'root', 1, '', ?, ?)`,
		name,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("server id: %v", err)
	}
	return id
}

func TestTokenStoreCreateListGetAndRevoke(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openTokenTestDB(t))
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)

	created, err := store.Create(ctx, CreateRequest{Name: "  cursor-maintenance  ", ExpiresAt: expiresAt})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if created.Name != "cursor-maintenance" {
		t.Fatalf("name was not trimmed: %q", created.Name)
	}
	if !strings.HasPrefix(created.TokenValue, "aip_") {
		t.Fatalf("unexpected token value: %q", created.TokenValue)
	}
	if created.TokenPrefix == "" || !strings.HasPrefix(created.TokenValue, created.TokenPrefix) {
		t.Fatalf("token prefix should match token value")
	}
	if created.ExpiresAt != expiresAt {
		t.Fatalf("expires_at should be normalized and returned, got %q", created.ExpiresAt)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got.TokenValue != "" {
		t.Fatalf("default token storage should be show-once")
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(list) != 1 || list[0].Name != "cursor-maintenance" || list[0].ExpiresAt != expiresAt {
		t.Fatalf("unexpected token list: %#v", list)
	}

	revoked, err := store.Revoke(ctx, created.ID)
	if err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if revoked.RevokedAt == "" {
		t.Fatalf("revoked_at should be set")
	}
	if _, err := store.Revoke(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTokenStoreEncryptsReusableTokenValueWhenReusableStorageIsEnabled(t *testing.T) {
	ctx := context.Background()
	database := openTokenTestDB(t)
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	store := NewStore(database, secretVault)

	created, err := store.Create(ctx, CreateRequest{Name: "codex"}, CreateOptions{StoreReusableToken: true})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	var tokenHash string
	var storedTokenValue string
	if err := database.QueryRowContext(ctx, `SELECT token_hash, token_value FROM api_tokens WHERE id = ?`, created.ID).Scan(&tokenHash, &storedTokenValue); err != nil {
		t.Fatalf("read raw token row: %v", err)
	}
	if tokenHash != HashToken(created.TokenValue) {
		t.Fatalf("token hash was not derived from token")
	}
	if strings.HasPrefix(storedTokenValue, "aip_") {
		t.Fatalf("token value should be encrypted at rest")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got.TokenValue != created.TokenValue {
		t.Fatalf("token value should decrypt for local reuse")
	}
}

func TestTokenStoreDoesNotReturnCiphertextWhenReusableTokenDecryptFails(t *testing.T) {
	ctx := context.Background()
	database := openTokenTestDB(t)
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	store := NewStore(database, secretVault)

	created, err := store.Create(ctx, CreateRequest{Name: "codex"}, CreateOptions{StoreReusableToken: true})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	var storedTokenValue string
	if err := database.QueryRowContext(ctx, `SELECT token_value FROM api_tokens WHERE id = ?`, created.ID).Scan(&storedTokenValue); err != nil {
		t.Fatalf("read raw token row: %v", err)
	}

	wrongVault, err := vault.New("different-gateway-secret")
	if err != nil {
		t.Fatalf("wrong vault: %v", err)
	}
	wrongStore := NewStore(database, wrongVault)
	got, err := wrongStore.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got.TokenValue != "" {
		t.Fatalf("decrypt failure should hide reusable token value, got %q from ciphertext %q", got.TokenValue, storedTokenValue)
	}
}

func TestTokenStoreValidatesCreate(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openTokenTestDB(t))

	if ValidationError("bad token").Error() != "bad token" {
		t.Fatalf("validation error should return message")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "  "}); err == nil {
		t.Fatalf("expected empty name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "bad\nname"}); err == nil {
		t.Fatalf("expected multiline name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: strings.Repeat("a", 81)}); err == nil {
		t.Fatalf("expected long name to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "expired", ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)}); err == nil {
		t.Fatalf("expected expired expires_at to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "bad expiry", ExpiresAt: "tomorrow"}); err == nil {
		t.Fatalf("expected invalid expires_at to fail")
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "same"}); err != nil {
		t.Fatalf("create first token: %v", err)
	}
	if _, err := store.Create(ctx, CreateRequest{Name: "same"}); err == nil {
		t.Fatalf("expected duplicate name to fail")
	}
	if _, err := store.Get(ctx, 123); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTokenPermissionsReplaceAndValidateMatrix(t *testing.T) {
	ctx := context.Background()
	database := openTokenTestDB(t)
	store := NewStore(database)
	token, err := store.Create(ctx, CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	betaID := insertTokenTestServer(t, database, "worker-beta")
	alphaID := insertTokenTestServer(t, database, "worker-alpha")
	permissionExpiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)

	permissions, err := store.UpdatePermissions(ctx, token.ID, UpdatePermissionsRequest{Permissions: []PermissionInput{
		{ServerID: betaID, ExecutionRule: RuleAlwaysRun, ExpiresAt: permissionExpiresAt},
		{ServerID: alphaID, ExecutionRule: RuleApprovalRequired},
	}})
	if err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	if len(permissions) != 2 {
		t.Fatalf("unexpected permission count: %#v", permissions)
	}
	if permissions[0].ServerName != "worker-alpha" || permissions[1].ServerName != "worker-beta" {
		t.Fatalf("permissions should be sorted by server name: %#v", permissions)
	}
	if permissions[1].ExpiresAt != permissionExpiresAt {
		t.Fatalf("permission expires_at should round-trip, got %#v", permissions[1])
	}

	permissions, err = store.UpdatePermissions(ctx, token.ID, UpdatePermissionsRequest{Permissions: []PermissionInput{
		{ServerID: alphaID, ExecutionRule: RuleBlocked},
	}})
	if err != nil {
		t.Fatalf("replace permissions: %v", err)
	}
	if len(permissions) != 1 || permissions[0].ExecutionRule != RuleBlocked {
		t.Fatalf("permissions were not replaced: %#v", permissions)
	}

	badInputs := []UpdatePermissionsRequest{
		{Permissions: []PermissionInput{{ServerID: 0, ExecutionRule: RuleAlwaysRun}}},
		{Permissions: []PermissionInput{{ServerID: alphaID, ExecutionRule: "root"}}},
		{Permissions: []PermissionInput{{ServerID: alphaID, ExecutionRule: RuleAlwaysRun, ExpiresAt: "tomorrow"}}},
		{Permissions: []PermissionInput{{ServerID: alphaID, ExecutionRule: RuleAlwaysRun, ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)}}},
		{Permissions: []PermissionInput{{ServerID: alphaID, ExecutionRule: RuleBlocked, ExpiresAt: permissionExpiresAt}}},
		{Permissions: []PermissionInput{{ServerID: alphaID, ExecutionRule: RuleAlwaysRun}, {ServerID: alphaID, ExecutionRule: RuleBlocked}}},
		{Permissions: []PermissionInput{{ServerID: 99999, ExecutionRule: RuleAlwaysRun}}},
	}
	for _, request := range badInputs {
		if _, err := store.UpdatePermissions(ctx, token.ID, request); err == nil {
			t.Fatalf("expected bad permission input to fail: %#v", request)
		}
	}
	if _, err := store.ListPermissions(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing token, got %v", err)
	}
}
