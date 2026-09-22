package securitypolicy

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	reusableTokensKey          = "reusable_tokens_enabled"
	exposeMCPServerMetadataKey = "expose_mcp_server_metadata"
	mcpStartEnabledKey         = "mcp_start_enabled"
	redactionModeKey           = "redaction_mode"
)

type settingValue struct {
	key   string
	value string
}

func readSettings(ctx context.Context, database sqldb.Executor) (Settings, error) {
	values := map[string]string{}
	for _, key := range []string{reusableTokensKey, exposeMCPServerMetadataKey, mcpStartEnabledKey, redactionModeKey} {
		var value string
		err := database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return Settings{}, err
		}
		values[key] = value
	}
	return NormalizeSettings(Settings{
		ReusableTokens:          values[reusableTokensKey] == "true",
		ExposeMCPServerMetadata: values[exposeMCPServerMetadataKey] == "true",
		MCPStartEnabled:         values[mcpStartEnabledKey] == "true",
		RedactionMode:           values[redactionModeKey],
	}), nil
}

func writeSettings(ctx context.Context, executor sqldb.Executor, settings Settings) error {
	settings = NormalizeSettings(settings)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, setting := range []settingValue{
		{key: reusableTokensKey, value: boolString(settings.ReusableTokens)},
		{key: exposeMCPServerMetadataKey, value: boolString(settings.ExposeMCPServerMetadata)},
		{key: mcpStartEnabledKey, value: boolString(settings.MCPStartEnabled)},
		{key: redactionModeKey, value: settings.RedactionMode},
	} {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			setting.key, setting.value, now,
		); err != nil {
			return err
		}
	}
	if settings.ReusableTokens {
		return nil
	}
	_, err := executor.ExecContext(ctx, `UPDATE api_tokens SET token_value = '', updated_at = ?`, now)
	return err
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
