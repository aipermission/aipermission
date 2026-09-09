package legacymigration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type migratedTargetProfile struct {
	TargetID  int64
	ProfileID int64
}

func copyLegacySettings(ctx context.Context, sourceDB *sql.DB, tx *sql.Tx) (int, error) {
	settings, err := legacySettings(ctx, sourceDB)
	if err != nil {
		return 0, err
	}
	for _, setting := range settings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			setting.Key, setting.Value, setting.UpdatedAt,
		); err != nil {
			return 0, fmt.Errorf("copy setting %q: %w", setting.Key, err)
		}
	}
	return len(settings), nil
}

func copyLegacySSHKeys(
	ctx context.Context,
	sourceDB *sql.DB,
	tx *sql.Tx,
	sourceVault, targetVault *vault.Vault,
) ([]legacySSHKey, map[int64]int64, error) {
	keys, err := legacySSHKeys(ctx, sourceDB)
	if err != nil {
		return nil, nil, err
	}
	keyIDMap := make(map[int64]int64, len(keys))
	for _, key := range keys {
		var secret privateKeySecret
		if err := sourceVault.DecryptJSON(key.EncryptedPrivateKey, &secret); err != nil {
			return nil, nil, fmt.Errorf("decrypt ssh key %q: %w", key.Name, err)
		}
		encrypted, err := targetVault.EncryptJSON(secret)
		if err != nil {
			return nil, nil, fmt.Errorf("encrypt ssh key %q: %w", key.Name, err)
		}
		inserted, err := tx.ExecContext(ctx, `
			INSERT INTO connector_credential_resources (
				connector_kind, resource_kind, name, resource_type, public_data,
				encrypted_secret, fingerprint, created_at, updated_at
			)
			VALUES ('ssh', 'private_key', ?, ?, ?, ?, ?, ?, ?)`,
			key.Name, key.KeyType, key.PublicKey, encrypted, key.Fingerprint, key.CreatedAt, key.UpdatedAt,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("copy ssh key %q: %w", key.Name, err)
		}
		id, err := inserted.LastInsertId()
		if err != nil {
			return nil, nil, err
		}
		keyIDMap[key.ID] = id
	}
	return keys, keyIDMap, nil
}

func copyLegacyTargets(
	ctx context.Context,
	sourceDB *sql.DB,
	tx *sql.Tx,
	keys []legacySSHKey,
	keyIDMap map[int64]int64,
) (map[int64]migratedTargetProfile, int, error) {
	servers, err := legacyServers(ctx, sourceDB)
	if err != nil {
		return nil, 0, err
	}
	store := connectortargets.NewTxStore(tx)
	profiles := make(map[int64]migratedTargetProfile, len(servers))
	for _, server := range servers {
		keyID := keyIDMap[server.SSHKeyID]
		if keyID == 0 {
			return nil, 0, fmt.Errorf("server %q references missing ssh key %d", server.Name, server.SSHKeyID)
		}
		key, err := sshKeyByID(keys, server.SSHKeyID)
		if err != nil {
			return nil, 0, err
		}
		target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
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
			return nil, 0, fmt.Errorf("create ssh connector target %q: %w", server.Name, err)
		}
		profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
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
			return nil, 0, fmt.Errorf("create ssh credential profile %q: %w", server.Name, err)
		}
		if _, err := store.EnsureRuntimeSurface(ctx, connectortargets.EnsureRuntimeSurfaceInput{
			ConnectorKind:  "ssh",
			TargetID:       target.ID,
			ProfileID:      profile.ID,
			CapabilityKind: connectortargets.RuntimeCapabilityLiveConsole,
			Label:          profile.Label,
		}); err != nil {
			return nil, 0, fmt.Errorf("create ssh runtime surface %q: %w", server.Name, err)
		}
		profiles[server.ID] = migratedTargetProfile{TargetID: target.ID, ProfileID: profile.ID}
	}
	return profiles, len(servers), nil
}

func copyLegacyTokens(ctx context.Context, sourceDB *sql.DB, tx *sql.Tx) (int, error) {
	tokens, err := legacyTokens(ctx, sourceDB)
	if err != nil {
		return 0, err
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
			return 0, fmt.Errorf("copy token %q: %w", token.Name, err)
		}
	}
	return len(tokens), nil
}

func copyLegacyPermissions(ctx context.Context, sourceDB *sql.DB, tx *sql.Tx, profiles map[int64]migratedTargetProfile) (int, error) {
	permissions, err := legacyPermissions(ctx, sourceDB)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, permission := range permissions {
		targetProfile := profiles[permission.ServerID]
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
			return 0, fmt.Errorf("copy token permission: %w", err)
		}
		copied++
	}
	return copied, nil
}
