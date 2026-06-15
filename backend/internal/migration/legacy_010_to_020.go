package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

var ErrTargetExists = errors.New("target database already exists")

type Legacy010To020Request struct {
	DataPath         string
	FallbackSecret   string
	SourceDatabaseID string
	SourcePassword   string
	TargetName       string
	TargetPassword   string
}

type Legacy010To020Result struct {
	Status             string `json:"status"`
	TargetDatabaseID   string `json:"target_database_id"`
	TargetDatabaseName string `json:"target_database_name"`
	SSHKeys            int    `json:"ssh_keys"`
	Targets            int    `json:"targets"`
	Tokens             int    `json:"tokens"`
	Permissions        int    `json:"permissions"`
	Settings           int    `json:"settings"`
	RedactionRules     int    `json:"redaction_rules"`
	Labels             int    `json:"labels"`
}

type legacySSHKey struct {
	ID                  int64
	Name                string
	KeyType             string
	PublicKey           string
	EncryptedPrivateKey string
	Fingerprint         string
	CreatedAt           string
	UpdatedAt           string
}

type legacyServer struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	SSHKeyID                 int64
	Description              string
	StartupInputAfterConnect string
	ForceShellCommand        string
	CreatedAt                string
	UpdatedAt                string
}

type legacyToken struct {
	ID          int64
	Name        string
	TokenHash   string
	TokenPrefix string
	TokenValue  string
	RevokedAt   string
	ExpiresAt   string
	CreatedAt   string
	UpdatedAt   string
}

type legacyPermission struct {
	TokenID       int64
	ServerID      int64
	ExecutionRule string
	ExpiresAt     string
	CreatedAt     string
	UpdatedAt     string
}

type privateKeySecret struct {
	PrivateKey string `json:"private_key"`
}

func MigrateLegacy010To020(ctx context.Context, request Legacy010To020Request) (Legacy010To020Result, error) {
	request.SourceDatabaseID = strings.TrimSpace(request.SourceDatabaseID)
	request.TargetName = strings.TrimSpace(request.TargetName)
	if request.SourcePassword == "" {
		return Legacy010To020Result{}, fmt.Errorf("source password is required")
	}
	if request.TargetPassword == "" {
		return Legacy010To020Result{}, fmt.Errorf("new database password is required")
	}
	if err := validateNewDatabasePassword(request.TargetPassword); err != nil {
		return Legacy010To020Result{}, err
	}
	if request.TargetName == "" {
		return Legacy010To020Result{}, fmt.Errorf("new database name is required")
	}
	sourcePath, err := db.DatabasePath(request.DataPath, request.SourceDatabaseID)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	if !db.Exists(sourcePath) {
		return Legacy010To020Result{}, fmt.Errorf("source database is not initialized")
	}
	targetID, targetPath, err := db.NewDatabasePath(request.DataPath, request.TargetName)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	if db.Exists(targetPath) {
		return Legacy010To020Result{}, ErrTargetExists
	}

	sourceDB, err := db.OpenEncryptedForMigration(sourcePath, request.SourcePassword)
	if err != nil {
		return Legacy010To020Result{}, fmt.Errorf("open source database: %w", err)
	}
	defer sourceDB.Close()
	if err := requireLegacySchema(ctx, sourceDB); err != nil {
		return Legacy010To020Result{}, err
	}
	sourceSecret, err := gatewaySecret(ctx, sourceDB, request.FallbackSecret)
	if err != nil {
		return Legacy010To020Result{}, err
	}

	targetDB, err := db.OpenEncrypted(targetPath, request.TargetPassword)
	if err != nil {
		_ = os.Remove(targetPath)
		return Legacy010To020Result{}, fmt.Errorf("create target database: %w", err)
	}
	defer targetDB.Close()
	if err := writeGatewaySecret(ctx, targetDB, sourceSecret); err != nil {
		_ = targetDB.Close()
		_ = os.Remove(targetPath)
		return Legacy010To020Result{}, err
	}

	result, err := migrateLegacyRows(ctx, sourceDB, targetDB, sourceSecret)
	if err != nil {
		_ = targetDB.Close()
		_ = os.Remove(targetPath)
		return Legacy010To020Result{}, err
	}
	result.Status = "completed"
	result.TargetDatabaseID = targetID
	result.TargetDatabaseName = strings.ReplaceAll(targetID, "-", " ")
	return result, nil
}

func migrateLegacyRows(ctx context.Context, sourceDB *sql.DB, targetDB *sql.DB, sourceSecret string) (Legacy010To020Result, error) {
	sourceVault, err := vault.New(sourceSecret)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	targetVault, err := vault.New(sourceSecret)
	if err != nil {
		return Legacy010To020Result{}, err
	}

	tx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return Legacy010To020Result{}, fmt.Errorf("begin target migration: %w", err)
	}
	defer tx.Rollback()
	txStore := connectortargets.NewTxStore(tx)

	result := Legacy010To020Result{}
	settings, err := legacySettings(ctx, sourceDB)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	for _, setting := range settings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			setting.Key,
			setting.Value,
			setting.UpdatedAt,
		); err != nil {
			return Legacy010To020Result{}, fmt.Errorf("copy setting %q: %w", setting.Key, err)
		}
		result.Settings++
	}

	keyIDMap := map[int64]int64{}
	keys, err := legacySSHKeys(ctx, sourceDB)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	for _, key := range keys {
		var secret privateKeySecret
		if err := sourceVault.DecryptJSON(key.EncryptedPrivateKey, &secret); err != nil {
			return Legacy010To020Result{}, fmt.Errorf("decrypt ssh key %q: %w", key.Name, err)
		}
		encrypted, err := targetVault.EncryptJSON(secret)
		if err != nil {
			return Legacy010To020Result{}, fmt.Errorf("encrypt ssh key %q: %w", key.Name, err)
		}
		inserted, err := tx.ExecContext(ctx, `
			INSERT INTO connector_credential_resources (
				connector_kind, resource_kind, name, resource_type, public_data,
				encrypted_secret, fingerprint, created_at, updated_at
			)
			VALUES ('ssh', 'private_key', ?, ?, ?, ?, ?, ?, ?)`,
			key.Name,
			key.KeyType,
			key.PublicKey,
			encrypted,
			key.Fingerprint,
			key.CreatedAt,
			key.UpdatedAt,
		)
		if err != nil {
			return Legacy010To020Result{}, fmt.Errorf("copy ssh key %q: %w", key.Name, err)
		}
		id, err := inserted.LastInsertId()
		if err != nil {
			return Legacy010To020Result{}, err
		}
		keyIDMap[key.ID] = id
		result.SSHKeys++
	}

	targetProfileByLegacyServer := map[int64]struct {
		TargetID  int64
		ProfileID int64
	}{}
	servers, err := legacyServers(ctx, sourceDB)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	for _, server := range servers {
		keyID := keyIDMap[server.SSHKeyID]
		if keyID == 0 {
			return Legacy010To020Result{}, fmt.Errorf("server %q references missing ssh key %d", server.Name, server.SSHKeyID)
		}
		key, err := sshKeyByID(keys, server.SSHKeyID)
		if err != nil {
			return Legacy010To020Result{}, err
		}
		target, err := txStore.CreateTarget(ctx, connectortargets.CreateTargetInput{
			ConnectorKind: "ssh",
			Name:          server.Name,
			Config: map[string]any{
				"host":                        server.Host,
				"port":                        server.Port,
				"description":                 server.Description,
				"startup_input_after_connect": server.StartupInputAfterConnect,
				"force_shell_command":         server.ForceShellCommand,
			},
		})
		if err != nil {
			return Legacy010To020Result{}, fmt.Errorf("create ssh connector target %q: %w", server.Name, err)
		}
		profile, err := txStore.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
			TargetID:      target.ID,
			ConnectorKind: "ssh",
			Kind:          "private_key",
			Label:         server.Username,
			Public: map[string]any{
				"username":    server.Username,
				"ssh_key_id":  keyID,
				"key_name":    key.Name,
				"key_type":    key.KeyType,
				"fingerprint": key.Fingerprint,
			},
		})
		if err != nil {
			return Legacy010To020Result{}, fmt.Errorf("create ssh credential profile %q: %w", server.Name, err)
		}
		if _, err := txStore.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
			ConnectorKind:  "ssh",
			TargetID:       target.ID,
			ProfileID:      profile.ID,
			CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
			Label:          profile.Label,
		}); err != nil {
			return Legacy010To020Result{}, fmt.Errorf("create ssh runtime surface %q: %w", server.Name, err)
		}
		targetProfileByLegacyServer[server.ID] = struct {
			TargetID  int64
			ProfileID int64
		}{TargetID: target.ID, ProfileID: profile.ID}
		result.Targets++
	}

	tokens, err := legacyTokens(ctx, sourceDB)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	for _, token := range tokens {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO api_tokens (
				id, name, token_hash, token_prefix, token_value, revoked_at,
				expires_at, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
			token.ID,
			token.Name,
			token.TokenHash,
			token.TokenPrefix,
			token.TokenValue,
			token.RevokedAt,
			token.ExpiresAt,
			token.CreatedAt,
			token.UpdatedAt,
		); err != nil {
			return Legacy010To020Result{}, fmt.Errorf("copy token %q: %w", token.Name, err)
		}
		result.Tokens++
	}

	permissions, err := legacyPermissions(ctx, sourceDB)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	for _, permission := range permissions {
		targetProfile := targetProfileByLegacyServer[permission.ServerID]
		if targetProfile.TargetID == 0 || targetProfile.ProfileID == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_connector_action_permissions (
				token_id, target_id, profile_id, action_name, execution_rule,
				expires_at, created_at, updated_at
			)
			VALUES (?, ?, ?, 'exec', ?, NULLIF(?, ''), ?, ?)`,
			permission.TokenID,
			targetProfile.TargetID,
			targetProfile.ProfileID,
			permission.ExecutionRule,
			permission.ExpiresAt,
			permission.CreatedAt,
			permission.UpdatedAt,
		); err != nil {
			return Legacy010To020Result{}, fmt.Errorf("copy token permission: %w", err)
		}
		result.Permissions++
	}

	copied, err := copyRedactionRules(ctx, sourceDB, tx)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	result.RedactionRules = copied
	copied, err = copyHistoryLabels(ctx, sourceDB, tx)
	if err != nil {
		return Legacy010To020Result{}, err
	}
	result.Labels = copied

	if err := tx.Commit(); err != nil {
		return Legacy010To020Result{}, fmt.Errorf("commit target migration: %w", err)
	}
	return result, nil
}

func requireLegacySchema(ctx context.Context, database *sql.DB) error {
	for _, table := range []string{"servers", "ssh_keys", "api_tokens", "token_server_permissions"} {
		exists, err := tableExists(ctx, database, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("source database is not a supported 0.1.x database; missing %s", table)
		}
	}
	return nil
}

type legacySetting struct {
	Key       string
	Value     string
	UpdatedAt string
}

func legacySettings(ctx context.Context, database *sql.DB) ([]legacySetting, error) {
	rows, err := database.QueryContext(ctx, `SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("read legacy settings: %w", err)
	}
	defer rows.Close()
	items := []legacySetting{}
	for rows.Next() {
		var item legacySetting
		if err := rows.Scan(&item.Key, &item.Value, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func gatewaySecret(ctx context.Context, database *sql.DB, fallback string) (string, error) {
	var value string
	err := database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'gateway_secret'`).Scan(&value)
	if err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read source gateway secret: %w", err)
	}
	if strings.TrimSpace(fallback) == "" {
		return "", fmt.Errorf("source gateway secret is missing")
	}
	return strings.TrimSpace(fallback), nil
}

func writeGatewaySecret(ctx context.Context, database *sql.DB, secret string) error {
	_, err := database.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ('gateway_secret', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		strings.TrimSpace(secret),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func legacySSHKeys(ctx context.Context, database *sql.DB) ([]legacySSHKey, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at
		FROM ssh_keys
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy ssh keys: %w", err)
	}
	defer rows.Close()
	items := []legacySSHKey{}
	for rows.Next() {
		var item legacySSHKey
		if err := rows.Scan(&item.ID, &item.Name, &item.KeyType, &item.PublicKey, &item.EncryptedPrivateKey, &item.Fingerprint, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func legacyServers(ctx context.Context, database *sql.DB) ([]legacyServer, error) {
	startupExpr := "''"
	if ok, _ := columnExists(ctx, database, "servers", "startup_input_after_connect"); ok {
		startupExpr = "startup_input_after_connect"
	}
	forceExpr := "''"
	if ok, _ := columnExists(ctx, database, "servers", "force_shell_command"); ok {
		forceExpr = "force_shell_command"
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, name, host, port, username, ssh_key_id, description, %s, %s, created_at, updated_at
		FROM servers
		ORDER BY id`, startupExpr, forceExpr))
	if err != nil {
		return nil, fmt.Errorf("read legacy servers: %w", err)
	}
	defer rows.Close()
	items := []legacyServer{}
	for rows.Next() {
		var item legacyServer
		if err := rows.Scan(&item.ID, &item.Name, &item.Host, &item.Port, &item.Username, &item.SSHKeyID, &item.Description, &item.StartupInputAfterConnect, &item.ForceShellCommand, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func legacyTokens(ctx context.Context, database *sql.DB) ([]legacyToken, error) {
	tokenValueExpr := "''"
	if ok, _ := columnExists(ctx, database, "api_tokens", "token_value"); ok {
		tokenValueExpr = "token_value"
	}
	expiresExpr := "''"
	if ok, _ := columnExists(ctx, database, "api_tokens", "expires_at"); ok {
		expiresExpr = "COALESCE(expires_at, '')"
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, name, token_hash, token_prefix, %s, COALESCE(revoked_at, ''), %s, created_at, updated_at
		FROM api_tokens
		ORDER BY id`, tokenValueExpr, expiresExpr))
	if err != nil {
		return nil, fmt.Errorf("read legacy tokens: %w", err)
	}
	defer rows.Close()
	items := []legacyToken{}
	for rows.Next() {
		var item legacyToken
		if err := rows.Scan(&item.ID, &item.Name, &item.TokenHash, &item.TokenPrefix, &item.TokenValue, &item.RevokedAt, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func legacyPermissions(ctx context.Context, database *sql.DB) ([]legacyPermission, error) {
	expiresExpr := "''"
	if ok, _ := columnExists(ctx, database, "token_server_permissions", "expires_at"); ok {
		expiresExpr = "COALESCE(expires_at, '')"
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf(`
		SELECT token_id, server_id, execution_rule, %s, created_at, updated_at
		FROM token_server_permissions
		ORDER BY id`, expiresExpr))
	if err != nil {
		return nil, fmt.Errorf("read legacy permissions: %w", err)
	}
	defer rows.Close()
	items := []legacyPermission{}
	for rows.Next() {
		var item legacyPermission
		if err := rows.Scan(&item.TokenID, &item.ServerID, &item.ExecutionRule, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func copyRedactionRules(ctx context.Context, source *sql.DB, target *sql.Tx) (int, error) {
	exists, err := tableExists(ctx, source, "redaction_rules")
	if err != nil || !exists {
		return 0, err
	}
	rows, err := source.QueryContext(ctx, `SELECT name, pattern, enabled, created_at, updated_at FROM redaction_rules ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var copied int
	for rows.Next() {
		var name, pattern, createdAt, updatedAt string
		var enabled int
		if err := rows.Scan(&name, &pattern, &enabled, &createdAt, &updatedAt); err != nil {
			return 0, err
		}
		if _, err := target.ExecContext(ctx, `
			INSERT OR IGNORE INTO redaction_rules (name, pattern, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)`, name, pattern, enabled, createdAt, updatedAt); err != nil {
			return 0, err
		}
		copied++
	}
	return copied, rows.Err()
}

func copyHistoryLabels(ctx context.Context, source *sql.DB, target *sql.Tx) (int, error) {
	exists, err := tableExists(ctx, source, "history_labels")
	if err != nil || !exists {
		return 0, err
	}
	rows, err := source.QueryContext(ctx, `SELECT name, color, created_at, updated_at FROM history_labels ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var copied int
	for rows.Next() {
		var name, color, createdAt, updatedAt string
		if err := rows.Scan(&name, &color, &createdAt, &updatedAt); err != nil {
			return 0, err
		}
		if _, err := target.ExecContext(ctx, `
			INSERT OR IGNORE INTO history_labels (name, color, created_at, updated_at)
			VALUES (?, ?, ?, ?)`, name, color, createdAt, updatedAt); err != nil {
			return 0, err
		}
		copied++
	}
	return copied, rows.Err()
}

func tableExists(ctx context.Context, database *sql.DB, table string) (bool, error) {
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
	return count > 0, err
}

func columnExists(ctx context.Context, database *sql.DB, table string, column string) (bool, error) {
	rows, err := database.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func sshKeyByID(keys []legacySSHKey, id int64) (legacySSHKey, error) {
	for _, key := range keys {
		if key.ID == id {
			return key, nil
		}
	}
	return legacySSHKey{}, fmt.Errorf("missing ssh key %d", id)
}

func validateNewDatabasePassword(password string) error {
	if len(password) < 14 {
		return fmt.Errorf("new database password must be at least 14 characters")
	}
	var hasUpper bool
	var hasLower bool
	var hasNumber bool
	for _, character := range password {
		switch {
		case character >= 'A' && character <= 'Z':
			hasUpper = true
		case character >= 'a' && character <= 'z':
			hasLower = true
		case character >= '0' && character <= '9':
			hasNumber = true
		}
	}
	if !hasUpper || !hasLower || !hasNumber {
		return fmt.Errorf("new database password must include uppercase letters, lowercase letters, and numbers")
	}
	return nil
}
